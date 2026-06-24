// Package sync runs imapsync child processes for queued accounts. The runner
// owns a worker pool bounded by settings.max_concurrent, streams each run's
// combined output to a per-operation log file and the event bus (SSE), and
// supports a hard Stop that kills every active child.
package sync

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"imap2gmail/internal/db/gen"
	"imap2gmail/internal/flags"
)

// BuildArgv constructs the imapsync argv from the global settings, the account,
// and the path to the 0600 access-token file. App-managed connection/auth flags
// are fixed; the global flag string contributes only behavior flags (re-validated
// here as a backstop); --dry is controlled solely by the dry_run setting.
func BuildArgv(s gen.Setting, account gen.Account, tokenFile string) ([]string, error) {
	extra, err := flags.Parse(s.ImapsyncFlags)
	if err != nil {
		return nil, fmt.Errorf("parse imapsync flags: %w", err)
	}
	if err := flags.ValidateTokens(extra); err != nil {
		return nil, err
	}

	argv := []string{"imapsync",
		"--host1", s.OriginHost,
		"--port1", fmt.Sprintf("%d", s.OriginPort),
	}
	if s.OriginSsl {
		argv = append(argv, "--ssl1")
	}
	argv = append(argv,
		"--user1", account.SourceUser,
		"--password1", account.SourcePassword,
		"--host2", "imap.gmail.com",
		"--port2", "993",
		"--ssl2",
		"--user2", account.DestGmail,
		"--oauthaccesstoken2", tokenFile,
		"--gmail2",
		"--nolog",
	)
	argv = append(argv, extra...)
	if s.DryRun {
		argv = append(argv, "--dry")
	}
	return argv, nil
}

// Run executes imapsync, streaming combined stdout/stderr line-by-line to onLine
// and appending to logPath. register is called with the started *exec.Cmd so the
// runner can kill it on Stop. Returns the cmd error (nil on success).
func Run(ctx context.Context, argv []string, logPath string, register func(*exec.Cmd), onLine func(string)) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// Put the child in its own process group so Stop can kill it + any forked
	// helpers (see process_unix.go), and bound I/O reaping so a stray child
	// holding the pipe can't hang Run() forever.
	setProcessGroup(cmd)
	cmd.WaitDelay = 30 * time.Second

	f, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create log: %w", err)
	}

	r, w := io.Pipe()
	cmd.Stdout = w
	cmd.Stderr = w

	if err := cmd.Start(); err != nil {
		w.Close()
		f.Close()
		return fmt.Errorf("start imapsync: %w", err)
	}
	register(cmd)

	var sw sync.WaitGroup
	sw.Go(func() {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			_, _ = f.WriteString(line + "\n")
			onLine(line)
		}
	})

	err = cmd.Wait()
	w.Close()
	sw.Wait()
	f.Close()
	return err
}
