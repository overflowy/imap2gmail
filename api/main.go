// Command imap2gmail runs a local single-binary server with an embedded web UI
// that bulk-syncs mailboxes from an origin IMAP server to Gmail via imapsync.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"imap2gmail/internal/config"
	"imap2gmail/internal/db"
	"imap2gmail/internal/db/gen"
	"imap2gmail/internal/events"
	"imap2gmail/internal/google"
	"imap2gmail/internal/sync"
	"imap2gmail/internal/web"
)

func main() {
	// Signal-aware context: cancelled on SIGINT/SIGTERM to stop the runner and
	// shut the server down gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	paths, err := config.Resolve()
	if err != nil {
		fatal(err)
	}
	if err := preflight(); err != nil {
		fatal(err)
	}

	d, err := db.Open(paths.DBPath)
	if err != nil {
		fatal(err)
	}
	defer d.Close()

	if err := db.Migrate(ctx, d); err != nil {
		fatal(fmt.Errorf("migrate: %w", err))
	}
	q := gen.New(d)
	if err := q.ResetRunningToIdle(ctx); err != nil {
		fatal(fmt.Errorf("reset running accounts: %w", err))
	}
	if err := config.TightenDB(paths.DBPath); err != nil {
		fatal(fmt.Errorf("chmod db: %w", err))
	}
	config.CleanRunDir()

	set, err := q.GetSettings(ctx)
	if err != nil {
		fatal(fmt.Errorf("load settings: %w", err))
	}

	bus := events.New()
	settingsFn := func() (gen.Setting, error) { return q.GetSettings(context.Background()) }
	runner := sync.New(ctx, sync.Deps{DB: d, Q: q, Bus: bus, Paths: paths, Settings: settingsFn})

	go sweeper(q)

	addr := fmt.Sprintf("127.0.0.1:%d", set.BindPort)
	srv := &http.Server{
		Addr:         addr,
		Handler:      web.New(d, q, bus, paths, runner, int(set.BindPort)).Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE streams indefinitely
		IdleTimeout:  60 * time.Second,
	}

	go openBrowser(int(set.BindPort))

	go func() {
		<-ctx.Done()
		runner.Stop()
		// Let the runner finalize active syncs (kill children + their process
		// groups, persist "stopped" status, append log terminators, remove token
		// files) before we tear the server down. Bounded so a stuck child can't
		// hang shutdown indefinitely.
		finalized := make(chan struct{})
		go func() { runner.Wait(); close(finalized) }()
		select {
		case <-finalized:
		case <-time.After(15 * time.Second):
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(shutdownCtx)
		cancel()
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fatal(fmt.Errorf("bind %s failed: %w\nChange bind_port in Settings and add the matching "+
			"http://127.0.0.1:<port>/ to Google Console → Authorized redirect URIs", addr, err))
	}
}

// preflight verifies imapsync is installed and on PATH.
func preflight() error {
	if _, err := exec.LookPath("imapsync"); err != nil {
		return fmt.Errorf("imapsync not found on PATH; install it from https://imapsync.lamiral.info/ (e.g. `brew install imapsync`)")
	}
	return nil
}

// sweeper periodically deletes expired OAuth nonces (older than 30 minutes).
func sweeper(q *gen.Queries) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)
		_ = q.SweepNonces(context.Background(), cutoff)
	}
}

// openBrowser opens the app URL in the default browser after a short delay.
func openBrowser(port int) {
	time.Sleep(300 * time.Millisecond)
	url := google.RedirectURL(port)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "imap2gmail:", err)
	os.Exit(1)
}
