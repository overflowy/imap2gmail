package csvimport

import "testing"

func TestParseComma(t *testing.T) {
	in := "source_user,password,gmail\nalice,pw1,a@gmail.com\nbob,pw2,b@gmail.com\n"
	r, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
	if r.Rows[0].SourceUser != "alice" || r.Rows[0].DestGmail != "a@gmail.com" {
		t.Errorf("row0 = %+v", r.Rows[0])
	}
}

func TestParseTab(t *testing.T) {
	in := "alice\tpw1\ta@gmail.com\nbob\tpw2\tb@gmail.com\n"
	r, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(r.Rows))
	}
}

func TestParseDedup(t *testing.T) {
	// Later duplicate source overwrites earlier within the batch.
	in := "alice,pw1,a@gmail.com\nalice,pw2,b@gmail.com\n"
	r, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 deduped row, got %d", len(r.Rows))
	}
	if r.Rows[0].SourcePassword != "pw2" || r.Rows[0].DestGmail != "b@gmail.com" {
		t.Errorf("expected later values to win, got %+v", r.Rows[0])
	}
}

func TestParseSkipsRagged(t *testing.T) {
	in := "alice,pw1,a@gmail.com\nonlytwo\nbob,pw2,b@gmail.com\n"
	r, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 {
		t.Fatalf("expected 2 valid rows, got %d", len(r.Rows))
	}
	if r.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", r.Skipped)
	}
}

func TestParseNoHeader(t *testing.T) {
	// Without a header-looking first line, the first row is data.
	in := "alice,pw1,a@gmail.com\n"
	r, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 1 {
		t.Fatalf("expected 1 row (no header), got %d", len(r.Rows))
	}
}
