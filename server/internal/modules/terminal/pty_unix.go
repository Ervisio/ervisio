//go:build unix

package terminal

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// ptyConn is the master side of a pty.
type ptyConn = *os.File

// startPTY starts cmd attached to a new pty of cols x rows. The pty makes the
// process a session leader, so cmd must not set SysProcAttr.
func startPTY(cmd *exec.Cmd, cols, rows uint16) (ptyConn, error) {
	return pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
}

// resizePTY changes the size of the pty.
func resizePTY(p ptyConn, cols, rows uint16) error {
	return pty.Setsize(p, &pty.Winsize{Cols: cols, Rows: rows})
}

// hangupProc sends SIGHUP to the process group of pid, and to pid itself when
// self is set.
func hangupProc(pid int, self bool) {
	_ = syscall.Kill(-pid, syscall.SIGHUP)
	if self {
		_ = syscall.Kill(pid, syscall.SIGHUP)
	}
}

// killProc sends SIGKILL to the process group of pid and to pid itself.
func killProc(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
