//go:build unix

package sys

import (
	"os"
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

func safePath() string { return "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" }

// keptEnv lists variables copied from the bridge environment.
var keptEnv = []string{"HOME", "USER", "LOGNAME", "SHELL", "TZ", "XDG_RUNTIME_DIR"}

// pathSeps are the separators that make a command name a path.
const pathSeps = "/"

// findExecutable returns p when it is a regular file with an execute bit.
func findExecutable(p string) (string, bool) {
	fi, err := os.Stat(p)
	return p, err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// extraLookup: nothing beyond SafePath on Unix.
func extraLookup(string) (string, bool) { return "", false }
