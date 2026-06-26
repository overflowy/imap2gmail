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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
func Run(ctx context.Context, argv []string, logPath string, register func(*exec.Cmd), onLine func(string, int64)) error {
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

	// Sample the child's resident set size every second. Logs stay as raw
	// imapsync output; RSS is reported separately for UI metadata and diagnosis.
	var rss atomic.Int64
	var maxRSS atomic.Int64
	if cmd.Process != nil {
		mem := processRSS(cmd.Process.Pid)
		rss.Store(mem)
		recordMaxRSS(&maxRSS, logPath, mem)
	}
	memDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-memDone:
				return
			case <-ticker.C:
				if cmd.Process != nil {
					mem := processRSS(cmd.Process.Pid)
					rss.Store(mem)
					recordMaxRSS(&maxRSS, logPath, mem)
				}
			}
		}
	}()

	var sw sync.WaitGroup
	sw.Go(func() {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			mem := rss.Load()
			_, _ = f.WriteString(line + "\n")
			onLine(line, mem)
		}
	})

	err = cmd.Wait()
	close(memDone)
	w.Close()
	sw.Wait()
	f.Close()
	if err != nil && ctx.Err() == nil {
		err = enrichFailure(cmd.ProcessState, err)
	}
	return err
}

func recordMaxRSS(maxRSS *atomic.Int64, logPath string, rss int64) {
	if rss <= 0 {
		return
	}
	for {
		cur := maxRSS.Load()
		if rss <= cur {
			return
		}
		if maxRSS.CompareAndSwap(cur, rss) {
			_ = os.WriteFile(rssPath(logPath), []byte(strconv.FormatInt(rss, 10)+"\n"), 0o600)
			return
		}
	}
}

func rssPath(logPath string) string {
	return strings.TrimSuffix(logPath, ".log") + ".rss"
}

// enrichFailure augments a non-app-cancelled imapsync failure with signal info.
// imapsync output is already streamed and recorded in the operation log.
func enrichFailure(ps *os.ProcessState, err error) error {
	var b strings.Builder
	b.WriteString(err.Error())
	signaled, sig := signalInfo(ps)
	switch {
	case signaled && sig == "killed":
		b.WriteString(" — killed by SIGKILL; check system logs for the cause (on macOS, memorystatus/no paging space records indicate an OS memory-pressure kill)")
	case signaled:
		fmt.Fprintf(&b, " — killed by signal %s", sig)
	}
	return fmt.Errorf("%s", b.String())
}

// processRSS returns the resident set size (in bytes) of the process with the
// given pid, or 0 if it cannot be determined. It shells out to ps (available on
// macOS and Linux), which reports RSS in KiB. The whole process group is not
// summed; imapsync's perl process holds the bulk of the memory.
func processRSS(pid int) int64 {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	kb, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || kb <= 0 {
		return 0
	}
	return kb * 1024
}
