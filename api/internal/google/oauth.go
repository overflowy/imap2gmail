// Package google wraps the OAuth2 flow against Google's endpoints for a Gmail
// destination: building the auth URL (with an opaque state nonce), exchanging
// the authorization code for tokens, and refreshing the short-lived access
// token from the stored refresh token.
package google

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Scope is the Gmail IMAP/SMTP scope requested from the user.
const Scope = "https://mail.google.com/"

// Config is an OAuth2 configuration bound to a client id/secret and a derived
// loopback redirect URL.
type Config struct {
	cfg *oauth2.Config
}

// RedirectURL derives the loopback OAuth redirect URL from the bind port. It is
// not stored: changing bind_port requires re-registering this URI in Google
// Console → Authorized redirect URIs.
func RedirectURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/", port)
}

// New builds a Config. redirectURL must be the derived http://127.0.0.1:<port>/.
func New(clientID, clientSecret, redirectURL string) *Config {
	return &Config{cfg: &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{Scope},
		Endpoint:     google.Endpoint,
	}}
}

// AuthURL returns the authorization URL for the given opaque state nonce. It
// requests offline access and forces consent so a refresh token is always
// returned.
func (c *Config) AuthURL(state string) string {
	return c.cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
}

// Exchange swaps an authorization code for OAuth2 tokens (refresh + access).
func (c *Config) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	tok, err := c.cfg.Exchange(ctx, code, oauth2.AccessTypeOffline)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	return tok, nil
}

// Refresh mints a fresh access token from a stored refresh token.
func (c *Config) Refresh(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("no refresh token")
	}
	ts := c.cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	tok, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	return tok, nil
}
