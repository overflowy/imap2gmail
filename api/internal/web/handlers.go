package web

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"imap2gmail/internal/csvimport"
	appdb "imap2gmail/internal/db"
	"imap2gmail/internal/db/gen"
	"imap2gmail/internal/events"
	"imap2gmail/internal/flags"
	"imap2gmail/internal/google"
	"imap2gmail/internal/sync"
)

// --- DTOs -------------------------------------------------------------------

type settingsDTO struct {
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	BindPort      int    `json:"bind_port"`
	OriginHost    string `json:"origin_host"`
	OriginPort    int    `json:"origin_port"`
	OriginSsl     bool   `json:"origin_ssl"`
	ImapsyncFlags string `json:"imapsync_flags"`
	DefaultFlags  string `json:"default_imapsync_flags"`
	MaxConcurrent int    `json:"max_concurrent"`
	DryRun        bool   `json:"dry_run"`
	RedirectURL   string `json:"redirect_url"` // derived, read-only
}

type accountDTO struct {
	ID             int64  `json:"id"`
	SourceUser     string `json:"source_user"`
	SourcePassword string `json:"source_password"`
	DestGmail      string `json:"dest_gmail"`
	SyncChecked    bool   `json:"sync_checked"`
	LastStatus     string `json:"last_status"`
	LastSyncedAt   string `json:"last_synced_at"`
	Authenticated  bool   `json:"authenticated"`
	AccessExpiry   string `json:"access_expiry"`
	Duplicate      bool   `json:"duplicate"`
}

type operationDTO struct {
	AccountID   int64  `json:"account_id"`
	OperationID string `json:"operation_id"`
	RSSBytes    int64  `json:"rss_bytes,omitempty"`
}

// --- Settings ---------------------------------------------------------------

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	set, err := s.q.GetSettings(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, toSettingsDTO(set))
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var dto settingsDTO
	if err := decodeJSON(r, &dto); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if dto.BindPort < 1 || dto.BindPort > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bind_port must be 1-65535"})
		return
	}
	if dto.MaxConcurrent < 1 || dto.MaxConcurrent > 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_concurrent must be 1-8"})
		return
	}
	if dto.OriginPort < 1 || dto.OriginPort > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "origin_port must be 1-65535"})
		return
	}
	if err := flags.Validate(dto.ImapsyncFlags); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	err := s.q.UpsertSettings(r.Context(), gen.UpsertSettingsParams{
		ClientID: dto.ClientID, ClientSecret: dto.ClientSecret,
		BindPort: int64(dto.BindPort), OriginHost: dto.OriginHost,
		OriginPort: int64(dto.OriginPort), OriginSsl: dto.OriginSsl,
		ImapsyncFlags: dto.ImapsyncFlags, MaxConcurrent: int64(dto.MaxConcurrent),
		DryRun: dto.DryRun,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	set, _ := s.q.GetSettings(r.Context())
	writeJSON(w, http.StatusOK, toSettingsDTO(set))
}

func toSettingsDTO(s gen.Setting) settingsDTO {
	return settingsDTO{
		ClientID: s.ClientID, ClientSecret: s.ClientSecret,
		BindPort: int(s.BindPort), OriginHost: s.OriginHost,
		OriginPort: int(s.OriginPort), OriginSsl: s.OriginSsl,
		ImapsyncFlags: s.ImapsyncFlags, DefaultFlags: appdb.DefaultImapsyncFlags,
		MaxConcurrent: int(s.MaxConcurrent),
		DryRun:        s.DryRun, RedirectURL: google.RedirectURL(int(s.BindPort)),
	}
}

// --- Accounts ---------------------------------------------------------------

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListAccounts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	dups := sync.DuplicateSources(rows)
	out := make([]accountDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAccountDTO(row, dups[row.SourceUser]))
	}
	writeJSON(w, http.StatusOK, out)
}

func toAccountDTO(row gen.ListAccountsRow, duplicate bool) accountDTO {
	return accountDTO{
		ID: row.ID, SourceUser: row.SourceUser, SourcePassword: row.SourcePassword,
		DestGmail: row.DestGmail, SyncChecked: row.SyncChecked,
		LastStatus: row.LastStatus, LastSyncedAt: row.LastSyncedAt,
		Authenticated: row.RefreshToken.Valid && row.RefreshToken.String != "",
		AccessExpiry:  nullStr(row.AccessExpiry),
		Duplicate:     duplicate,
	}
}

func nullStr(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}

func (s *Server) addAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourceUser     string `json:"source_user"`
		SourcePassword string `json:"source_password"`
		DestGmail      string `json:"dest_gmail"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.SourceUser == "" || body.DestGmail == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source_user and dest_gmail are required"})
		return
	}
	if err := s.upsertDestination(r, body.DestGmail); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	acc, err := s.q.InsertAccount(r.Context(), gen.InsertAccountParams{
		SourceUser: body.SourceUser, SourcePassword: body.SourcePassword,
		DestGmail: body.DestGmail, SyncChecked: true, LastStatus: "idle", LastSyncedAt: "",
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": acc.ID})
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body struct {
		SourceUser     string `json:"source_user"`
		SourcePassword string `json:"source_password"`
		DestGmail      string `json:"dest_gmail"`
		SyncChecked    bool   `json:"sync_checked"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	existing, err := s.q.GetAccount(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}
	if err := s.upsertDestination(r, body.DestGmail); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	err = s.q.UpdateAccount(r.Context(), gen.UpdateAccountParams{
		SourceUser: body.SourceUser, SourcePassword: body.SourcePassword,
		DestGmail: body.DestGmail, SyncChecked: body.SyncChecked,
		LastStatus: existing.LastStatus, LastSyncedAt: existing.LastSyncedAt, ID: id,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := s.q.DeleteAccount(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) setChecked(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body struct {
		Checked bool `json:"checked"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.q.SetAccountChecked(r.Context(), gen.SetAccountCheckedParams{SyncChecked: body.Checked, ID: id}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) checkAll(w http.ResponseWriter, r *http.Request) {
	s.setAll(w, r, true)
}
func (s *Server) checkNone(w http.ResponseWriter, r *http.Request) {
	s.setAll(w, r, false)
}

func (s *Server) setAll(w http.ResponseWriter, r *http.Request, checked bool) {
	if err := s.q.SetAllChecked(r.Context(), checked); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Import -----------------------------------------------------------------

func (s *Server) importAccounts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	res, err := csvimport.Parse(body.Text)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	qtx := s.q.WithTx(tx)
	for _, row := range res.Rows {
		_ = qtx.UpsertDestination(r.Context(), row.DestGmail)
		matches, _ := qtx.GetAccountsBySource(r.Context(), row.SourceUser)
		if len(matches) == 1 {
			a := matches[0]
			_ = qtx.UpdateAccount(r.Context(), gen.UpdateAccountParams{
				SourceUser: a.SourceUser, SourcePassword: row.SourcePassword,
				DestGmail: row.DestGmail, SyncChecked: a.SyncChecked,
				LastStatus: a.LastStatus, LastSyncedAt: a.LastSyncedAt, ID: a.ID,
			})
		} else {
			_, _ = qtx.InsertAccount(r.Context(), gen.InsertAccountParams{
				SourceUser: row.SourceUser, SourcePassword: row.SourcePassword,
				DestGmail: row.DestGmail, SyncChecked: true, LastStatus: "idle", LastSyncedAt: "",
			})
		}
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rows, _ := s.q.ListAccounts(r.Context())
	dups := sync.DuplicateSources(rows)
	accounts := make([]accountDTO, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, toAccountDTO(row, dups[row.SourceUser]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"imported": len(res.Rows),
		"skipped":  res.Skipped,
		"errors":   res.Errors,
		"accounts": accounts,
	})
}

// --- Auth -------------------------------------------------------------------

func (s *Server) authURL(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	account, err := s.q.GetAccount(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}
	set, err := s.q.GetSettings(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if set.ClientID == "" || set.ClientSecret == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set OAuth client_id and client_secret in Settings first"})
		return
	}
	nonce := randToken()
	_ = s.q.CreateNonce(r.Context(), gen.CreateNonceParams{
		Nonce: nonce, DestGmail: account.DestGmail, CreatedAt: nowRFC3339(),
	})
	cfg := google.New(set.ClientID, set.ClientSecret, google.RedirectURL(int(set.BindPort)))
	writeJSON(w, http.StatusOK, map[string]string{"auth_url": cfg.AuthURL(nonce)})
}

func (s *Server) authExchange(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code is required"})
		return
	}
	account, err := s.q.GetAccount(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}
	set, err := s.q.GetSettings(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cfg := google.New(set.ClientID, set.ClientSecret, google.RedirectURL(int(set.BindPort)))
	tok, err := cfg.Exchange(r.Context(), body.Code)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "exchange failed: " + err.Error()})
		return
	}
	refresh := tok.RefreshToken
	if refresh == "" {
		if d, derr := s.q.GetDestination(r.Context(), account.DestGmail); derr == nil {
			refresh = d.RefreshToken
		}
	}
	expiry := ""
	if !tok.Expiry.IsZero() {
		expiry = tok.Expiry.Format(time.RFC3339)
	}
	_ = s.q.SetDestinationTokens(r.Context(), gen.SetDestinationTokensParams{
		RefreshToken: refresh, AccessToken: tok.AccessToken,
		AccessExpiry: expiry, Gmail: account.DestGmail,
	})
	s.bus.Publish(events.Event{Type: "auth-ok", DestGmail: account.DestGmail, Timestamp: nowRFC3339()})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Sync -------------------------------------------------------------------

func (s *Server) syncOne(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	account, err := s.q.GetAccount(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}
	if dup, reason := s.isDuplicate(r, account.SourceUser); dup {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": reason})
		return
	}
	if err := s.runner.SyncOne(id); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"queued": true})
}

func (s *Server) syncAll(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListAccounts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	dups := sync.DuplicateSources(rows)
	var ids []int64
	skipped := 0
	for _, row := range rows {
		if !row.SyncChecked {
			continue
		}
		if dups[row.SourceUser] {
			_ = s.q.SetAccountStatus(r.Context(), gen.SetAccountStatusParams{LastStatus: "skipped", ID: row.ID})
			s.bus.Publish(events.Event{
				Type: "status", AccountID: row.ID, SourceUser: row.SourceUser,
				Status: "skipped", Reason: "duplicate source", Timestamp: nowRFC3339(),
			})
			skipped++
			continue
		}
		ids = append(ids, row.ID)
	}
	if err := s.runner.Start(ids); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"queued": len(ids), "skipped": skipped})
}

func (s *Server) syncStop(w http.ResponseWriter, r *http.Request) {
	s.runner.Stop()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Logs / operations ------------------------------------------------------

func (s *Server) listAccountLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	acc, err := s.q.GetAccount(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}
	ops := listOpsForAccount(s.paths.AccountLogDir(acc.SourceUser))
	writeJSON(w, http.StatusOK, ops)
}

func (s *Server) getAccountLog(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	acc, err := s.q.GetAccount(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}
	ts := r.URL.Query().Get("ts")
	if ts == "" || strings.ContainsAny(ts, "/\\") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ts"})
		return
	}
	path := filepath.Join(s.paths.AccountLogDir(acc.SourceUser), ts+".log")
	data, err := os.ReadFile(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "log not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": string(data)})
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.q.ListAccounts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, []operationDTO{})
		return
	}
	ops := []operationDTO{}
	for _, acc := range accounts {
		dir := s.paths.AccountLogDir(acc.SourceUser)
		for _, op := range listOpsForAccount(dir) {
			ops = append(ops, operationDTO{AccountID: acc.ID, OperationID: op, RSSBytes: readOperationRSS(dir, op)})
		}
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].OperationID > ops[j].OperationID })
	if len(ops) > 50 {
		ops = ops[:50]
	}
	writeJSON(w, http.StatusOK, ops)
}

// listOpsForAccount returns operation ids (log filenames without .log) for one
// account's log directory, newest first.
func listOpsForAccount(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}
	}
	ops := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".log") {
			continue
		}
		ops = append(ops, strings.TrimSuffix(name, ".log"))
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ops)))
	return ops
}

func readOperationRSS(dir, op string) int64 {
	data, err := os.ReadFile(filepath.Join(dir, op+".rss"))
	if err != nil {
		return 0
	}
	rss, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || rss <= 0 {
		return 0
	}
	return rss
}

// --- Helpers ----------------------------------------------------------------

func (s *Server) upsertDestination(r *http.Request, gmail string) error {
	return s.q.UpsertDestination(r.Context(), gmail)
}

// isDuplicate reports whether the source_user is duplicated across accounts.
func (s *Server) isDuplicate(r *http.Request, sourceUser string) (bool, string) {
	rows, err := s.q.ListAccounts(r.Context())
	if err != nil {
		return false, ""
	}
	dups := sync.DuplicateSources(rows)
	if dups[sourceUser] {
		return true, "duplicate source: resolve duplicate Source Mailboxes before syncing"
	}
	return false, ""
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return 0, false
	}
	return id, true
}
