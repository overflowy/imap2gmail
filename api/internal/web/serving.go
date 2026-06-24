package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"imap2gmail/internal/db/gen"
	"imap2gmail/internal/events"
	"imap2gmail/internal/google"
)

// handleRootOrAsset serves the SPA shell at "/", static assets at other paths
// (falling back to the shell for client-side routing), and intercepts the OAuth
// callback when code&state are present.
func (s *Server) handleRootOrAsset(w http.ResponseWriter, r *http.Request) {
	s.ensureCSRF(w, r)
	if r.URL.Path == "/" {
		if code := r.URL.Query().Get("code"); code != "" {
			s.oauthCallback(w, r, code)
			return
		}
		s.serveIndex(w, r)
		return
	}
	s.serveAsset(w, r)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := distFS.ReadFile("assets/dist/index.html")
	if err != nil {
		http.Error(w, "frontend not built (run `task build`)", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(distFS, "assets/dist")
	if err != nil {
		s.serveIndex(w, r)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/")
	if f, err := sub.Open(p); err == nil {
		_ = f.Close()
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
		return
	}
	// Unknown path → SPA shell (client-side routing).
	s.serveIndex(w, r)
}

// ensureCSRF sets a fresh double-submit CSRF cookie if none is present. The
// cookie is readable by JS (not HttpOnly) so the SPA can echo it in the
// X-CSRF-Token header; SameSite=Lax blocks cross-site submission.
func (s *Server) ensureCSRF(w http.ResponseWriter, r *http.Request) {
	if c, _ := r.Cookie(csrfCookie); c != nil && c.Value != "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    randToken(),
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
}

func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// oauthCallback exchanges the authorization code for tokens, stores them on the
// destination bound to the state nonce, then redirects into the SPA.
func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request, code string) {
	state := r.URL.Query().Get("state")
	nonce, err := s.q.GetNonce(r.Context(), state)
	if err != nil {
		redirectAuth(w, "err", "invalid or expired state")
		return
	}
	set, err := s.q.GetSettings(r.Context())
	if err != nil {
		redirectAuth(w, "err", "settings unavailable")
		return
	}
	cfg := google.New(set.ClientID, set.ClientSecret, google.RedirectURL(int(set.BindPort)))
	tok, err := cfg.Exchange(r.Context(), code)
	if err != nil {
		redirectAuth(w, "err", "token exchange failed")
		return
	}
	refresh := ""
	if tok.RefreshToken != "" {
		refresh = tok.RefreshToken
	} else {
		// Preserve an existing refresh token if Google did not return a new one.
		if d, derr := s.q.GetDestination(r.Context(), nonce.DestGmail); derr == nil {
			refresh = d.RefreshToken
		}
	}
	expiry := ""
	if !tok.Expiry.IsZero() {
		expiry = tok.Expiry.Format(time.RFC3339)
	}
	_ = s.q.SetDestinationTokens(r.Context(), gen.SetDestinationTokensParams{
		RefreshToken: refresh,
		AccessToken:  tok.AccessToken,
		AccessExpiry: expiry,
		Gmail:        nonce.DestGmail,
	})
	_ = s.q.DeleteNonce(r.Context(), state)
	s.bus.Publish(events.Event{Type: "auth-ok", DestGmail: nonce.DestGmail, Timestamp: nowRFC3339()})
	redirectAuth(w, "ok", nonce.DestGmail)
}

func redirectAuth(w http.ResponseWriter, status, info string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><meta http-equiv="refresh" content="0; url=/?auth=%s&info=%s">`, status, info)
}

// handleSSE streams events to the client. Optional ?account={id} scopes the
// stream to one account's operations.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	var filter int64 = -1
	if a := r.URL.Query().Get("account"); a != "" {
		if v, err := strconv.ParseInt(a, 10, 64); err == nil {
			filter = v
		}
	}

	ch, cancel := s.bus.Subscribe()
	defer cancel()

	// Flush headers + greet.
	fmt.Fprintf(w, "event: hello\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if filter >= 0 && ev.AccountID != filter && ev.AccountID != 0 {
				continue
			}
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
			flusher.Flush()
		}
	}
}

// nowRFC3339 returns the current UTC time in RFC3339.
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// decodeJSON decodes a JSON request body into v.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
