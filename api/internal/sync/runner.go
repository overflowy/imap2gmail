package sync

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"imap2gmail/internal/config"
	"imap2gmail/internal/db/gen"
	"imap2gmail/internal/events"
	"imap2gmail/internal/google"
)

// Deps are the runner's external dependencies.
type Deps struct {
	DB       *sql.DB
	Q        *gen.Queries
	Bus      *events.Bus
	Paths    *config.Paths
	Settings func() (gen.Setting, error) // read current global settings
	// Mint produces a fresh access token from a refresh token. If nil, the
	// default Google OAuth2 implementation is used. Injectable for tests.
	Mint func(ctx context.Context, s gen.Setting, refreshToken string) (accessToken string, expiry time.Time, err error)
}

// mint resolves the configured token minter (or the default Google one).
func (r *Runner) mint(ctx context.Context, s gen.Setting, refreshToken string) (string, time.Time, error) {
	if r.deps.Mint != nil {
		return r.deps.Mint(ctx, s, refreshToken)
	}
	return defaultMint(ctx, s, refreshToken)
}

// defaultMint refreshes via Google OAuth2 using the app's client credentials.
func defaultMint(ctx context.Context, s gen.Setting, refreshToken string) (string, time.Time, error) {
	cfg := google.New(s.ClientID, s.ClientSecret, google.RedirectURL(int(s.BindPort)))
	tok, err := cfg.Refresh(ctx, refreshToken)
	if err != nil {
		return "", time.Time{}, err
	}
	return tok.AccessToken, tok.Expiry, nil
}

// Runner is the global sync orchestrator. At most one run is active at a time;
// a run processes a batch of account IDs with a worker pool bounded by
// settings.max_concurrent.
type Runner struct {
	deps   Deps
	parent context.Context

	mu        sync.Mutex
	running   bool
	runCtx    context.Context
	runCancel context.CancelFunc
	active    map[int64]*exec.Cmd
	activeOps map[int64]string
	runDone   chan struct{} // closed when the current run has fully drained
}

// New creates a Runner. parent is cancelled on app shutdown to stop all runs.
func New(parent context.Context, deps Deps) *Runner {
	return &Runner{deps: deps, parent: parent}
}

// IsRunning reports whether a sync run is currently in progress.
func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// IsOperationActive reports whether the given account operation is still owned
// by the runner. This includes setup/teardown time around the imapsync process.
func (r *Runner) IsOperationActive(accountID int64, operationID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.activeOps != nil && r.activeOps[accountID] == operationID
}

// Wait blocks until the current run (if any) has fully finished — every active
// imapsync child killed, terminal status persisted, log terminator written, and
// token file removed. Returns immediately when no run is active. Used during
// graceful shutdown so SIGTERM finalizes instead of being cut off mid-cleanup.
func (r *Runner) Wait() {
	r.mu.Lock()
	ch := r.runDone
	r.mu.Unlock()
	if ch != nil {
		<-ch
	}
}

// Start queues the given account IDs for a new run. It returns an error if a
// run is already in progress.
func (r *Runner) Start(ids []int64) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("a sync is already in progress")
	}
	r.mu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	go r.run(ids)
	return nil
}

// SyncOne starts a single-account run.
func (r *Runner) SyncOne(id int64) error {
	return r.Start([]int64{id})
}

// Stop kills every active child (and its process group) and lets the run
// finalize. In-flight accounts are marked stopped; not-yet-dispatched accounts
// stay idle. Cancelling runCtx also triggers exec.CommandContext's own kill as a
// backstop; killGroup ensures forked helpers die too so the log scanner unblocks.
func (r *Runner) Stop() {
	r.mu.Lock()
	if r.runCancel != nil {
		r.runCancel()
	}
	for _, cmd := range r.active {
		killGroup(cmd)
	}
	r.mu.Unlock()
}

func (r *Runner) run(ids []int64) {
	s, err := r.deps.Settings()
	if err != nil {
		return
	}
	maxConc := min(max(int(s.MaxConcurrent), 1), 8)

	runCtx, cancel := context.WithCancel(r.parent)
	done := make(chan struct{})
	r.mu.Lock()
	r.running = true
	r.runCtx = runCtx
	r.runCancel = cancel
	r.active = make(map[int64]*exec.Cmd)
	r.activeOps = make(map[int64]string)
	r.runDone = done
	r.mu.Unlock()

	// Finalize exactly once: cancel the run, reset state, and signal Wait().
	defer func() {
		cancel()
		r.mu.Lock()
		r.running = false
		r.runCtx = nil
		r.runCancel = nil
		r.active = nil
		r.activeOps = nil
		r.runDone = nil
		r.mu.Unlock()
		close(done)
	}()

	sem := make(chan struct{}, maxConc)
	var wg sync.WaitGroup

	for _, id := range ids {
		select {
		case sem <- struct{}{}:
		case <-runCtx.Done():
			goto wait
		}
		if runCtx.Err() != nil {
			<-sem
			goto wait
		}
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			defer func() { <-sem }()
			r.syncAccount(runCtx, id)
		}(id)
	}

wait:
	wg.Wait()
}

func (r *Runner) syncAccount(runCtx context.Context, accountID int64) {
	dbc := context.Background()
	account, err := r.deps.Q.GetAccount(dbc, accountID)
	if err != nil {
		return
	}
	sourceUser := account.SourceUser
	opID := opStamp()
	r.mu.Lock()
	if r.activeOps != nil {
		r.activeOps[accountID] = opID
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		if r.activeOps != nil && r.activeOps[accountID] == opID {
			delete(r.activeOps, accountID)
		}
		r.mu.Unlock()
	}()

	r.deps.Bus.Publish(events.Event{
		Type: "operation", AccountID: accountID, SourceUser: sourceUser,
		OperationID: opID, Timestamp: nowRFC3339(),
	})

	logPath, logErr := r.deps.Paths.LogPath(sourceUser, opID)
	if logErr != nil {
		r.logLine(accountID, sourceUser, opID, "", "==== FAILED: create log dir: "+logErr.Error()+" ====")
		r.mark(dbc, accountID, sourceUser, opID, "failed", "create log dir: "+logErr.Error())
		return
	}

	dest, err := r.deps.Q.GetDestination(dbc, account.DestGmail)
	if err != nil || dest.RefreshToken == "" {
		r.logLine(accountID, sourceUser, opID, logPath, "==== SKIPPED: destination not authenticated ====")
		r.mark(dbc, accountID, sourceUser, opID, "skipped", "not authenticated")
		return
	}
	if runCtx.Err() != nil {
		r.logLine(accountID, sourceUser, opID, logPath, "==== STOPPED: aborted before start ====")
		r.mark(dbc, accountID, sourceUser, opID, "stopped", "")
		return
	}

	r.mark(dbc, accountID, sourceUser, opID, "running", "")

	s, err := r.deps.Settings()
	if err != nil {
		r.logLine(accountID, sourceUser, opID, logPath, "==== FAILED: read settings: "+err.Error()+" ====")
		r.mark(dbc, accountID, sourceUser, opID, "failed", "read settings: "+err.Error())
		return
	}
	access, expiryTime, err := r.mint(runCtx, s, dest.RefreshToken)
	if err != nil {
		// A cancellation during refresh is a stop, not a failure.
		if runCtx.Err() != nil {
			r.logLine(accountID, sourceUser, opID, logPath, "==== STOPPED: aborted during auth ====")
			r.mark(dbc, accountID, sourceUser, opID, "stopped", "")
		} else {
			r.logLine(accountID, sourceUser, opID, logPath, "==== FAILED: token refresh: "+err.Error()+" ====")
			r.mark(dbc, accountID, sourceUser, opID, "failed", "token refresh: "+err.Error())
		}
		return
	}
	expiry := ""
	if !expiryTime.IsZero() {
		expiry = expiryTime.Format(time.RFC3339)
	}
	_ = r.deps.Q.SetDestinationTokens(dbc, gen.SetDestinationTokensParams{
		RefreshToken: dest.RefreshToken,
		AccessToken:  access,
		AccessExpiry: expiry,
		Gmail:        account.DestGmail,
	})

	tokenFile := r.deps.Paths.TokenFilePath(accountID)
	if err := os.WriteFile(tokenFile, []byte(access+"\n"), 0o600); err != nil {
		r.logLine(accountID, sourceUser, opID, logPath, "==== FAILED: write token file: "+err.Error()+" ====")
		r.mark(dbc, accountID, sourceUser, opID, "failed", "write token file: "+err.Error())
		return
	}
	pidFile := r.deps.Paths.PidFilePath(accountID, opID)

	refCtx, refCancel := context.WithCancel(runCtx)
	go r.refreshLoop(refCtx, dest.RefreshToken, tokenFile, account.DestGmail)

	argv, err := BuildArgv(s, account, tokenFile, pidFile)
	if err != nil {
		refCancel()
		_ = os.Remove(tokenFile)
		_ = os.Remove(pidFile)
		r.logLine(accountID, sourceUser, opID, logPath, "==== FAILED: "+err.Error()+" ====")
		r.mark(dbc, accountID, sourceUser, opID, "failed", err.Error())
		return
	}

	runErr := Run(runCtx, argv, logPath, func(cmd *exec.Cmd) {
		r.mu.Lock()
		r.active[accountID] = cmd
		r.mu.Unlock()
	}, func(line string, rssBytes int64) {
		r.deps.Bus.Publish(events.Event{
			Type: "log", AccountID: accountID, SourceUser: sourceUser,
			OperationID: opID, Line: line, RSSBytes: rssBytes, Timestamp: nowRFC3339(),
		})
	})

	refCancel()
	_ = os.Remove(tokenFile)
	_ = os.Remove(pidFile)
	r.mu.Lock()
	delete(r.active, accountID)
	r.mu.Unlock()

	switch {
	case runCtx.Err() != nil:
		r.logLine(accountID, sourceUser, opID, logPath, "==== STOPPED: sync aborted ====")
		r.mark(dbc, accountID, sourceUser, opID, "stopped", "")
	case runErr != nil:
		r.logLine(accountID, sourceUser, opID, logPath, "==== FAILED: "+runErr.Error()+" ====")
		r.mark(dbc, accountID, sourceUser, opID, "failed", runErr.Error())
	default:
		r.logLine(accountID, sourceUser, opID, logPath, "==== COMPLETED: imapsync exited 0 ====")
		_ = r.deps.Q.SetAccountSynced(dbc, gen.SetAccountSyncedParams{
			LastStatus: "ok", LastSyncedAt: nowRFC3339(), ID: accountID,
		})
		r.deps.Bus.Publish(events.Event{
			Type: "status", AccountID: accountID, SourceUser: sourceUser,
			OperationID: opID, Status: "ok", Timestamp: nowRFC3339(),
		})
	}
}

// logLine appends a line to the operation's log file (creating it if needed)
// and publishes it as a "log" SSE event so the output pane shows it live. It is
// only called outside an active Run() — for pre-run reasons or post-run
// terminators — so it never races with the scanner that owns the file during a
// run. If logPath is "" (e.g. the log dir could not be created), only the SSE
// event is emitted.
func (r *Runner) logLine(accountID int64, sourceUser, opID, logPath, line string) {
	if logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, _ = f.WriteString(line + "\n")
			_ = f.Close()
		}
	}
	r.deps.Bus.Publish(events.Event{
		Type: "log", AccountID: accountID, SourceUser: sourceUser,
		OperationID: opID, Line: line, Timestamp: nowRFC3339(),
	})
}

// refreshLoop re-mints the access token and rewrites the token file (0600) on a
// short interval so long single-account syncs survive the ~1h token expiry.
// imapsync re-reads the file on reconnect.
func (r *Runner) refreshLoop(ctx context.Context, refreshToken, tokenFile, gmail string) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s, err := r.deps.Settings()
			if err != nil {
				continue
			}
			access, expiryTime, err := r.mint(ctx, s, refreshToken)
			if err != nil {
				continue
			}
			_ = os.WriteFile(tokenFile, []byte(access+"\n"), 0o600)
			expiry := ""
			if !expiryTime.IsZero() {
				expiry = expiryTime.Format(time.RFC3339)
			}
			_ = r.deps.Q.SetDestinationTokens(context.Background(), gen.SetDestinationTokensParams{
				RefreshToken: refreshToken,
				AccessToken:  access,
				AccessExpiry: expiry,
				Gmail:        gmail,
			})
		}
	}
}

// mark sets an account's status and emits the corresponding SSE event.
func (r *Runner) mark(ctx context.Context, id int64, sourceUser, opID, status, reason string) {
	_ = r.deps.Q.SetAccountStatus(ctx, gen.SetAccountStatusParams{LastStatus: status, ID: id})
	r.deps.Bus.Publish(events.Event{
		Type: "status", AccountID: id, SourceUser: sourceUser,
		OperationID: opID, Status: status, Reason: reason, Timestamp: nowRFC3339(),
	})
}

// DuplicateSources returns the set of source_user values that appear more than
// once across the given accounts. Members of a duplicate group are blocked from
// syncing (marked skipped) and flagged red in the UI.
func DuplicateSources(rows []gen.ListAccountsRow) map[string]bool {
	counts := make(map[string]int)
	for _, r := range rows {
		counts[r.SourceUser]++
	}
	dups := make(map[string]bool)
	for u, c := range counts {
		if c > 1 {
			dups[u] = true
		}
	}
	return dups
}

func opStamp() string {
	return time.Now().UTC().Format("2006-01-02T15-04-05.000000000Z")
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
