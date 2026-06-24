package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open opens (or creates) the SQLite database at path with the connection-level
// pragmas required by the app: foreign_keys = ON (so ON DELETE CASCADE fires)
// and journal_mode = WAL (concurrent reads during writes). A busy timeout makes
// the pooled connections tolerate brief write contention.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite handles concurrent reads well in WAL mode, but only one writer at a
	// time. Limit the pool so writers queue rather than erroring on busy.
	d.SetMaxOpenConns(1)
	if err := d.PingContext(context.Background()); err != nil {
		d.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return d, nil
}
