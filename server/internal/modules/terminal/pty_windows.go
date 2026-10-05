//go:build windows

package terminal

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

// ptyConn is the master side of a pty. A ConPTY implementation will provide
// it; until then startPTY never returns one.
type ptyConn = io.ReadWriteCloser

var errPTYUnsupported = errors.New("pseudo-terminals are not supported on windows yet")

// startPTY is where ConPTY goes. For now it refuses, so no terminal is
// started without one.
func startPTY(cmd *exec.Cmd, cols, rows uint16) (ptyConn, error) {
	return nil, errPTYUnsupported
}

// resizePTY changes the size of the pty.
func resizePTY(p ptyConn, cols, rows uint16) error {
	return errPTYUnsupported
}

// hangupProc has no equivalent on windows; the process is killed by killProc.
func hangupProc(pid int, self bool) {}

// killProc kills the process.
func killProc(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

// isPTYClosed reports whether a read error just means the pty ended.
func isPTYClosed(err error) bool { return false }
