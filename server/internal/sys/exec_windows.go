//go:build windows

package sys

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts the command in a new process group and kills it
// when its context ends. Child processes are not tracked yet: that needs a
// job object.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil
	}
}
