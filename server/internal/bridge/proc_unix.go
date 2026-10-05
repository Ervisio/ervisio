//go:build unix

package bridge

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/ervisio/ervisio/server/internal/account"
)

// setBridgeAttr starts cmd in a new session: no controlling terminal (so sudo
// cannot prompt on a tty) and no terminal signals from the daemon's console.
// With switchUser it runs with the account's uid/gid/groups.
func setBridgeAttr(cmd *exec.Cmd, switchUser bool, a *account.Account) {
	attr := &syscall.SysProcAttr{
		Setsid:    true,
		Pdeathsig: syscall.SIGTERM,
	}
	if switchUser {
		attr.Credential = &syscall.Credential{
			Uid:    a.UID,
			Gid:    a.GID,
			Groups: a.Groups,
		}
	}
	cmd.SysProcAttr = attr
}

// setSessionAttr starts cmd in a new session that dies with the daemon.
func setSessionAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGTERM}
}

// setUserAttr makes cmd run as account a.
func setUserAttr(cmd *exec.Cmd, a *account.Account) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: a.UID, Gid: a.GID, Groups: a.Groups},
		Pdeathsig:  syscall.SIGTERM,
	}
}

// notifySignals forwards termination signals to sigs and ignores SIGPIPE.
func notifySignals(sigs chan<- os.Signal) {
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	signal.Ignore(syscall.SIGPIPE)
}

// killProcessGroup kills cmd's process group, falling back to the process.
func killProcessGroup(cmd *exec.Cmd) {
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ESRCH) {
		_ = cmd.Process.Kill()
	}
}
