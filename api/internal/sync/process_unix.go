//go:build unix

package sync

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup puts the imapsync child in its own process group so that (1) a
// terminal Ctrl-C does not signal imapsync directly (our signal handler manages
// it), and (2) Stop can kill the whole group — imapsync (perl) may fork helper
// children that inherit the stdout pipe; killing only the direct process would
// orphan them and keep the output pipe open, hanging the log scanner.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup sends SIGKILL to the child's whole process group (imapsync + any
// forked helpers), guaranteeing prompt termination and that inherited pipes
// close so the streaming scanner reaches EOF.
func killGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// Negative PID signals the process group whose ID == -pid (the child's,
	// since Setpgid made it a leader with pgid == pid).
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// signalInfo reports whether the process was terminated by a signal and, if so,
// the signal name (e.g. "killed" for SIGKILL, "terminated" for SIGTERM). Used
// to distinguish an external kill (e.g. the OS out-of-memory killer) from a
// normal non-zero exit.
func signalInfo(ps *os.ProcessState) (signaled bool, sigName string) {
	if ps == nil {
		return false, ""
	}
	ws, ok := ps.Sys().(syscall.WaitStatus)
	if !ok {
		return false, ""
	}
	if !ws.Signaled() {
		return false, ""
	}
	return true, ws.Signal().String()
}
