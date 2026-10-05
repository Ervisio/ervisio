//go:build windows

package bridge

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"

	"github.com/ervisio/ervisio/server/internal/account"
)

var errUserSwitchUnsupported = errors.New("running a process as another user is not supported on windows yet")

// setBridgeAttr refuses to run as another user (CreateProcessAsUser comes
// later); cmd.Err makes Start fail instead of silently keeping our identity.
func setBridgeAttr(cmd *exec.Cmd, switchUser bool, _ *account.Account) {
	if switchUser {
		cmd.Err = errUserSwitchUnsupported
	}
}

// setSessionAttr is a no-op on windows.
func setSessionAttr(*exec.Cmd) {}

// setUserAttr refuses to run as another user on windows.
func setUserAttr(cmd *exec.Cmd, _ *account.Account) {
	cmd.Err = errUserSwitchUnsupported
}

func notifySignals(sigs chan<- os.Signal) {
	signal.Notify(sigs, os.Interrupt)
}

func killProcessGroup(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
