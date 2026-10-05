//go:build unix

package sys

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the command in its own process group and kills the
// whole group when its context ends, so helpers it spawned go too.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
}
