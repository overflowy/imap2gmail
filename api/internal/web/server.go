// Package web serves the embedded SPA, the JSON API, and the SSE event stream.
// It binds 127.0.0.1 only and applies Host/Origin + double-submit CSRF
// middleware to protect the plaintext credentials in the DB.
package web

import (
	"database/sql"
	"embed"
	"net/http"
	"strings"

	"imap2gmail/internal/config"
	"imap2gmail/internal/db/gen"
	"imap2gmail/internal/events"
	"imap2gmail/internal/sync"
)

//go:embed assets/dist
var distFS embed.FS

// Server is the HTTP server holding shared dependencies.
type Server struct {
	db     *sql.DB
	q      *gen.Queries
	bus    *events.Bus
	paths  *config.Paths
	runner *sync.Runner
	port   int
}

// New creates a Server.
func New(db *sql.DB, q *gen.Queries, bus *events.Bus, paths *config.Paths, runner *sync.Runner, port int) *Server {
	return &Server{db: db, q: q, bus: bus, paths: paths, runner: runner, port: port}
}

// Handler returns the HTTP handler with all routes and middleware wired.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", s.handleRootOrAsset)

	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("POST /api/settings", s.saveSettings)

	mux.HandleFunc("GET /api/accounts", s.listAccounts)
	mux.HandleFunc("POST /api/accounts", s.addAccount)
	mux.HandleFunc("POST /api/accounts/import", s.importAccounts)
	mux.HandleFunc("POST /api/accounts/check-all", s.checkAll)
	mux.HandleFunc("POST /api/accounts/check-none", s.checkNone)

	mux.HandleFunc("GET /api/accounts/{id}/logs", s.listAccountLogs)
	mux.HandleFunc("GET /api/accounts/{id}/log", s.getAccountLog)
	mux.HandleFunc("PUT /api/accounts/{id}", s.updateAccount)
	mux.HandleFunc("DELETE /api/accounts/{id}", s.deleteAccount)
	mux.HandleFunc("POST /api/accounts/{id}/checked", s.setChecked)
	mux.HandleFunc("POST /api/accounts/{id}/auth", s.authURL)
	mux.HandleFunc("POST /api/accounts/{id}/auth/exchange", s.authExchange)
	mux.HandleFunc("POST /api/accounts/{id}/sync", s.syncOne)

	mux.HandleFunc("POST /api/sync", s.syncAll)
	mux.HandleFunc("POST /api/sync/stop", s.syncStop)
	mux.HandleFunc("GET /api/operations", s.listOperations)

	mux.HandleFunc("GET /events", s.handleSSE)

	return s.middleware(mux)
}

// middleware enforces loopback Host/Origin and a double-submit CSRF token on
// state-changing requests.
func (s *Server) middleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "forbidden: non-loopback host", http.StatusForbidden)
			return
		}
		if isStateChanging(r.Method) {
			if origin := r.Header.Get("Origin"); origin != "" && !isLoopbackOrigin(origin) {
				http.Error(w, "forbidden: non-loopback origin", http.StatusForbidden)
				return
			}
			cookie, _ := r.Cookie(csrfCookie)
			header := r.Header.Get(csrfHeader)
			if cookie == nil || cookie.Value == "" || header == "" || cookie.Value != header {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

const (
	csrfCookie = "csrf"
	csrfHeader = "X-CSRF-Token"
)

func isStateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// isLoopbackHost reports whether Host (host:port) names a loopback address.
func isLoopbackHost(host string) bool {
	h := host
	if i := strings.LastIndex(h, ":"); i >= 0 {
		// strip port (also handles [::1]:port)
		if strings.HasPrefix(h, "[") {
			h = h[:strings.Index(h, "]")+1]
			h = strings.Trim(h, "[]")
		} else {
			h = h[:i]
		}
	}
	switch h {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// isLoopbackOrigin reports whether an Origin/Referer URL names a loopback host.
func isLoopbackOrigin(raw string) bool {
	rest := raw
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	host := rest
	if strings.Contains(host, "[") {
		host = strings.Trim(strings.SplitN(strings.SplitN(host, "]", 2)[0], "[", 2)[1], "[]")
	} else if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}
