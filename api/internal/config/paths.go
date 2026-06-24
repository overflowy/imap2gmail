// Package config resolves the on-disk paths the app uses, all relative to the
// binary's current working directory: ./data/db/data.db (secrets, 0600),
// ./data/logs/<account_id>/ (per-operation sync logs), and ./run/ (0700,
// transient 0600 token files passed to imapsync).
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	dataDir  = "data"
	dbDir    = "data/db"
	dbFile   = "data/db/data.db"
	logsDir  = "data/logs"
	runDir   = "run"
	logsPerm = 0o700
	runPerm  = 0o700
	dataPerm = 0o700
)

// Paths are the resolved on-disk locations used by the app.
type Paths struct {
	DataDir string
	DBPath  string
	LogsDir string
	RunDir  string
}

// Resolve creates the required directories (with restrictive permissions) and
// returns the path set. The DB file itself is tightened to 0600 after first open
// (see TightenDB in the db package caller).
func Resolve() (*Paths, error) {
	for _, d := range []string{dataDir, dbDir, logsDir, runDir} {
		if err := os.MkdirAll(d, dataPerm); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", d, err)
		}
		// MkdirAll does not change perms of an existing dir; enforce explicitly.
		_ = os.Chmod(d, dataPerm)
	}
	return &Paths{
		DataDir: dataDir,
		DBPath:  dbFile,
		LogsDir: logsDir,
		RunDir:  runDir,
	}, nil
}

// LogPath returns the full path for a given account's operation log, identified
// by an RFC3339 timestamp string. The account's log directory is created lazily.
func (p *Paths) LogPath(accountID int64, ts string) (string, error) {
	dir := filepath.Join(logsDir, fmt.Sprintf("%d", accountID))
	if err := os.MkdirAll(dir, logsPerm); err != nil {
		return "", err
	}
	return filepath.Join(dir, ts+".log"), nil
}

// AccountLogDir returns the directory holding one account's operation logs.
func (p *Paths) AccountLogDir(accountID int64) string {
	return filepath.Join(logsDir, fmt.Sprintf("%d", accountID))
}

// TokenFilePath returns a 0600-destined path for an account's access-token file.
func (p *Paths) TokenFilePath(accountID int64) string {
	return filepath.Join(runDir, fmt.Sprintf("token-%d.txt", accountID))
}

// TightenDB sets the DB file mode to 0600 (it holds plaintext secrets).
func TightenDB(path string) error {
	return os.Chmod(path, 0o600)
}

// CleanRunDir removes stale transient access-token files left behind by a
// previous crash/kill. They are 0600 and hold short-lived tokens, but tidying
// them on startup avoids leaving credentials on disk.
func CleanRunDir() {
	matches, _ := filepath.Glob(filepath.Join(runDir, "token-*.txt"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}
