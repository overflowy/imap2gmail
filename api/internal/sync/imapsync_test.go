package sync

import (
	"slices"
	"testing"

	"imap2gmail/internal/db/gen"
)

func sampleSetting() gen.Setting {
	return gen.Setting{
		ID: 1, ClientID: "cid", ClientSecret: "sec", BindPort: 8080,
		OriginHost: "imap.example.com", OriginPort: 993, OriginSsl: true,
		ImapsyncFlags: "--automap --useheader Message-Id",
		MaxConcurrent: 2, DryRun: true,
	}
}

func sampleAccount() gen.Account {
	return gen.Account{
		ID: 7, SourceUser: "alice@origin", SourcePassword: "pw",
		DestGmail: "alice@gmail.com", SyncChecked: true, LastStatus: "idle",
	}
}

func contains(argv []string, token string) bool {
	return slices.Contains(argv, token)
}

func TestBuildArgvBasic(t *testing.T) {
	argv, err := BuildArgv(sampleSetting(), sampleAccount(), "/run/token-7.txt")
	if err != nil {
		t.Fatalf("BuildArgv error: %v", err)
	}
	if argv[0] != "imapsync" {
		t.Errorf("argv[0] = %q, want imapsync", argv[0])
	}
	want := []string{
		"imapsync",
		"--host1", "imap.example.com",
		"--port1", "993",
		"--ssl1",
		"--user1", "alice@origin",
		"--password1", "pw",
		"--host2", "imap.gmail.com",
		"--port2", "993",
		"--ssl2",
		"--user2", "alice@gmail.com",
		"--oauthaccesstoken2", "/run/token-7.txt",
		"--gmail2",
		"--nolog",
		"--automap", "--useheader", "Message-Id",
		"--dry",
	}
	if !eqStr(argv, want) {
		t.Errorf("argv = %v\nwant   = %v", argv, want)
	}
}

func TestBuildArgvNoSslNoDry(t *testing.T) {
	s := sampleSetting()
	s.OriginSsl = false
	s.DryRun = false
	s.ImapsyncFlags = "--automap"
	argv, err := BuildArgv(s, sampleAccount(), "/run/t.txt")
	if err != nil {
		t.Fatalf("BuildArgv error: %v", err)
	}
	if contains(argv, "--ssl1") {
		t.Error("did not expect --ssl1 when OriginSsl=false")
	}
	if contains(argv, "--dry") {
		t.Error("did not expect --dry when DryRun=false")
	}
	if !contains(argv, "--ssl2") {
		t.Error("expected --ssl2 (Gmail is always SSL)")
	}
	if !contains(argv, "--gmail2") {
		t.Error("expected --gmail2")
	}
	if !contains(argv, "--nolog") {
		t.Error("expected --nolog to suppress LOG_imapsync/ file logging")
	}
}

func TestBuildArgvDenylistBackstop(t *testing.T) {
	s := sampleSetting()
	s.ImapsyncFlags = "--automap --password1 evil"
	if _, err := BuildArgv(s, sampleAccount(), "/run/t.txt"); err == nil {
		t.Error("BuildArgv should reject denied flags as a backstop")
	}
}

func TestBuildArgvQuotedFlag(t *testing.T) {
	s := sampleSetting()
	s.DryRun = false
	s.ImapsyncFlags = `--regextrans2 's/foo/bar/'`
	argv, err := BuildArgv(s, sampleAccount(), "/run/t.txt")
	if err != nil {
		t.Fatalf("BuildArgv error: %v", err)
	}
	if !contains(argv, "s/foo/bar/") {
		t.Errorf("expected quoted flag value preserved, got %v", argv)
	}
}

func TestDuplicateSources(t *testing.T) {
	rows := []gen.ListAccountsRow{
		{ID: 1, SourceUser: "a"},
		{ID: 2, SourceUser: "a"}, // dup
		{ID: 3, SourceUser: "b"},
		{ID: 4, SourceUser: "c"},
		{ID: 5, SourceUser: "c"}, // dup
	}
	dups := DuplicateSources(rows)
	if !dups["a"] || !dups["c"] {
		t.Errorf("expected a and c to be duplicates, got %v", dups)
	}
	if dups["b"] {
		t.Error("b should not be a duplicate")
	}
	if len(dups) != 2 {
		t.Errorf("expected 2 duplicate sources, got %d", len(dups))
	}
}

func eqStr(a, b []string) bool {
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
