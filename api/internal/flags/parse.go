// Package flags tokenizes the operator-editable global imapsync flag string with
// a quote-aware splitter (so values like --regextrans2 's/foo/bar/' survive) and
// validates it against a denylist of app-managed flags. The flag string is never
// passed to a shell; tokens are appended verbatim to exec.Command args.
package flags

import (
	"fmt"
	"strings"

	"github.com/google/shlex"
)

// DeniedFlags are owned by the app/checkbox and must not appear in the global
// flag string: connection, credentials, identity (source + destination), OAuth
// tokens/refresh, admin-auth users, the Gmail-mode switch, dry-run, and file
// logging (the app captures combined output itself, so imapsync's
// LOG_imapsync/ directory logging is suppressed via --nolog and must stay off).
var DeniedFlags = map[string]struct{}{
	"--host1":             {},
	"--port1":             {},
	"--ssl1":              {},
	"--user1":             {},
	"--password1":         {},
	"--oauthaccesstoken1": {},
	"--oauthdirect1":      {},
	"--oauthrefreshcmd1":  {},
	"--authuser1":         {},
	"--host2":             {},
	"--port2":             {},
	"--ssl2":              {},
	"--user2":             {},
	"--password2":         {},
	"--authmech2":         {},
	"--authuser2":         {},
	"--oauthaccesstoken2": {},
	"--oauthdirect2":      {},
	"--oauthrefreshcmd2":  {},
	"--gmail2":            {},
	"--dry":               {},
	"--log":               {},
	"--nolog":             {},
	"--logfile":           {},
	"--logdir":            {},
}

// Parse tokenizes the flag string (POSIX-ish: single/double quotes and
// backslash escapes) into an argv slice. It never invokes a shell.
func Parse(s string) ([]string, error) {
	return shlex.Split(s)
}

// flagName extracts the leading --name from a token, normalizing both
// "--flag value" and "--flag=value" forms to "--flag".
func flagName(token string) string {
	if !strings.HasPrefix(token, "--") {
		return ""
	}
	if before, _, ok := strings.Cut(token, "="); ok {
		return before
	}
	return token
}

// ValidateTokens returns an error if any token's flag name is on the denylist.
func ValidateTokens(tokens []string) error {
	for _, t := range tokens {
		name := flagName(t)
		if name == "" {
			continue
		}
		if _, denied := DeniedFlags[name]; denied {
			return fmt.Errorf("flag %s is managed by the app and cannot be set in the global flag string", name)
		}
	}
	return nil
}

// Validate parses and validates the flag string. Used on settings save and as a
// backstop at sync build time.
func Validate(s string) error {
	tokens, err := Parse(s)
	if err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	return ValidateTokens(tokens)
}
