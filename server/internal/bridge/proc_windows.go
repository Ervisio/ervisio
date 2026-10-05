//go:build windows

package bridge

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/pam"
)

var (
	errUserSwitchUnsupported = errors.New("running a process as another user is not supported on windows yet")
	errNoUserToken           = errors.New("no logon token for this account: sign in again")
)

// setBridgeAttr starts cmd with the signed-in user's primary token (the one
// pam.Authenticate kept from LogonUser; Go then calls CreateProcessAsUser).
// Without a token cmd.Err makes Start fail: the bridge never silently runs
// under the daemon's own (SYSTEM) identity. Admin unlock (sudo) has no
// Windows equivalent yet: StartAdmin's sudo fails, and a linked elevated
// token (TokenLinkedToken) is the intended follow-up.
// Not covered here: the account package still resolves users via
// uid/gid/shell, so the daemon's login path needs a Windows account
// resolver before this is reachable end to end.
func setBridgeAttr(cmd *exec.Cmd, switchUser bool, a *account.Account) {
	if !switchUser {
		return
	}
	if a == nil {
		cmd.Err = errUserSwitchUnsupported
		return
	}
	tok, ok := pam.UserToken(a.Name)
	if !ok {
		cmd.Err = errNoUserToken
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Token: tok, HideWindow: true}
}

// setSessionAttr is a no-op on windows.
func setSessionAttr(*exec.Cmd) {}

// setUserAttr (the root session helper) is unix-only; refuse on windows.
func setUserAttr(cmd *exec.Cmd, _ *account.Account) {
	cmd.Err = errUserSwitchUnsupported
}

func notifySignals(sigs chan<- os.Signal) {
	signal.Notify(sigs, os.Interrupt)
}

func killProcessGroup(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
