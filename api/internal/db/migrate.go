package db

import (
	"context"
	"database/sql"
	_ "embed"
	"strings"
)

//go:embed schema.sql
var schemaSQL string

// Migrate applies the schema (idempotent CREATE TABLE IF NOT EXISTS) and ensures
// the singleton settings row (id = 1) exists.
func Migrate(ctx context.Context, d *sql.DB) error {
	for _, stmt := range splitStatements(renderSchema(schemaSQL)) {
		if _, err := d.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	const seed = `INSERT INTO settings (id) VALUES (1) ON CONFLICT(id) DO NOTHING`
	if _, err := d.ExecContext(ctx, seed); err != nil {
		return err
	}
	return nil
}

func renderSchema(s string) string {
	return strings.ReplaceAll(s, "'__DEFAULT_IMAPSYNC_FLAGS__'", sqlString(DefaultImapsyncFlags))
}

func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// splitStatements breaks a SQL script into individual statements, ignoring
// semicolons inside single-quoted string literals, -- line comments, and
// /* */ block comments.
func splitStatements(s string) []string {
	var (
		out     []string
		cur     strings.Builder
		inStr   bool
		inLine  bool
		inBlock bool
		prev    byte
	)
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, t)
		}
		cur.Reset()
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inLine:
			cur.WriteByte(c)
			if c == '\n' {
				inLine = false
			}
		case inBlock:
			cur.WriteByte(c)
			if prev == '*' && c == '/' {
				inBlock = false
			}
		case inStr:
			cur.WriteByte(c)
			if c == '\'' {
				inStr = false
			}
		case c == '\'':
			cur.WriteByte(c)
			inStr = true
		case c == '-' && prev == '-':
			// replace the '-' we already wrote; it stays as part of comment line
			cur.WriteByte(c)
			inLine = true
		case c == '*' && prev == '/':
			cur.WriteByte(c)
			inBlock = true
		case c == ';':
			flush()
		default:
			cur.WriteByte(c)
		}
		prev = c
	}
	flush()
	return out
}
