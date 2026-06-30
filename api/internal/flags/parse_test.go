package flags

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"--automap --useheader Message-Id", []string{"--automap", "--useheader", "Message-Id"}},
		{`--regextrans2 's/foo/bar/'`, []string{"--regextrans2", "s/foo/bar/"}},
		{`--exclude "INBOX/Spam" --maxage 30`, []string{"--exclude", "INBOX/Spam", "--maxage", "30"}},
		{"", []string{}},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", c.in, err)
		}
		if !eq(got, c.want) {
			t.Errorf("Parse(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := []string{"--automap", "--syncinternaldates --useheader Message-Id", "--regextrans2 's/a/b/' --maxage 30"}
	for _, s := range ok {
		if err := Validate(s); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", s, err)
		}
	}
	bad := []string{
		"--password1 hack",       // denied, space form
		"--host1=evil",           // denied, = form
		"--automap --dry",        // --dry is app-managed
		"--oauthaccesstoken2 /x", // denied
		"--gmail2",               // denied
		"--automap --log",        // file logging app-managed
		"--nolog",                // file logging app-managed
		"--logdir /tmp/x",        // file logging app-managed
		"--pidfile /tmp/x.pid",   // pid files app-managed
		"--pidfilelocking",       // pid files app-managed
	}
	for _, s := range bad {
		if err := Validate(s); err == nil {
			t.Errorf("Validate(%q) = nil, want error", s)
		}
	}
}

func TestValidateAllowedAuthmech1(t *testing.T) {
	// Origin CRAM-MD5 flags are NOT denied (only host1 OAuth/admin flags are).
	if err := Validate("--authmech1 CRAM-MD5 --authmd51"); err != nil {
		t.Errorf("Validate(allowed origin auth flags) = %v, want nil", err)
	}
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
