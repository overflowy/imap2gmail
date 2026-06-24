// Package csvimport parses pasted/CSV account text into validated rows.
// Accepts comma- or tab-separated input, with an optional header line, and
// deduplicates by Source Mailbox (later rows overwrite earlier ones in the same
// batch).
package csvimport

import (
	"encoding/csv"
	"errors"
	"io"
	"strings"
)

// Row is a parsed, trimmed account import record.
type Row struct {
	SourceUser     string
	SourcePassword string
	DestGmail      string
}

// Result holds the parsed rows and per-line warnings.
type Result struct {
	Rows    []Row
	Skipped int      // lines that failed validation
	Errors  []string // per-line error messages (1:1 with skipped order)
}

// Parse parses the input text. Each data record must have at least 3 fields:
// source_user, source_password, dest_gmail. Password may be empty.
func Parse(input string) (*Result, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("no input")
	}

	delim := detectDelimiter(input)
	r := csv.NewReader(strings.NewReader(input))
	r.Comma = delim
	r.FieldsPerRecord = -1 // tolerate ragged rows; we validate field count ourselves
	r.TrimLeadingSpace = true

	var (
		out     []Row
		byUser  = make(map[string]int) // source_user -> index in out
		skipped int
		errs    []string
		lineNo  int
	)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			skipped++
			errs = append(errs, err.Error())
			continue
		}
		lineNo++
		if lineNo == 1 && isHeader(rec) {
			continue
		}
		if len(rec) < 3 {
			skipped++
			errs = append(errs, "line needs 3 fields: source_user,password,gmail")
			continue
		}
		user := strings.TrimSpace(rec[0])
		pass := strings.TrimSpace(rec[1])
		gmail := strings.TrimSpace(rec[2])
		if user == "" || gmail == "" {
			skipped++
			errs = append(errs, "source_user and dest_gmail are required")
			continue
		}
		row := Row{SourceUser: user, SourcePassword: pass, DestGmail: gmail}
		if idx, ok := byUser[user]; ok {
			out[idx] = row // later duplicate overwrites earlier
		} else {
			byUser[user] = len(out)
			out = append(out, row)
		}
	}
	return &Result{Rows: out, Skipped: skipped, Errors: errs}, nil
}

// detectDelimiter picks tab if the first non-empty line has a tab, else comma.
func detectDelimiter(s string) rune {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, "\t") {
			return '\t'
		}
		return ','
	}
	return ','
}

// isHeader returns true if the record looks like a header row.
func isHeader(rec []string) bool {
	joined := strings.ToLower(strings.Join(rec, ","))
	return strings.Contains(joined, "source_user") ||
		(strings.Contains(joined, "gmail") && strings.Contains(joined, "password"))
}
