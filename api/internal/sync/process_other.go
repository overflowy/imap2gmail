//go:build !unix

package sync

import "os/exec"

// setProcessGroup is a no-op on non-Unix platforms (no process-group support);
// the default per-process kill is used.
func setProcessGroup(cmd *exec.Cmd) {}

// killGroup falls back to killing the direct process on non-Unix platforms.
func killGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
