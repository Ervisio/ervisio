//go:build windows

package terminal

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func defaultHome() string {
	if h := os.Getenv("USERPROFILE"); h != "" {
		return h
	}
	return `C:\`
}

func shellArgv(base string) []string { return []string{base} }

// lookSSH finds the OpenSSH client that ships with Windows.
func lookSSH() (string, error) {
	if p, err := exec.LookPath("ssh.exe"); err == nil {
		return p, nil
	}
	root := os.Getenv("SYSTEMROOT")
	if root == "" {
		root = `C:\Windows`
	}
	p := filepath.Join(root, "System32", "OpenSSH", "ssh.exe")
	if isFile(p) {
		return p, nil
	}
	return "", rpc.Errorf(rpc.Unavailable, "The ssh client is not installed. Install the OpenSSH client feature and try again.")
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// resolveShell picks the requested shell when it is an existing .exe, else
// pwsh, Windows PowerShell or cmd.
func resolveShell(want, _ string) (string, error) {
	if want != "" {
		if !filepath.IsAbs(want) || !strings.EqualFold(filepath.Ext(want), ".exe") || !isFile(want) {
			return "", rpc.Errorf(rpc.Invalid, "%s is not an existing shell executable (.exe).", want)
		}
		return want, nil
	}
	for _, n := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	root := os.Getenv("SYSTEMROOT")
	if root == "" {
		root = `C:\Windows`
	}
	ps := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if isFile(ps) {
		return ps, nil
	}
	if c := os.Getenv("COMSPEC"); c != "" {
		return c, nil
	}
	return filepath.Join(root, "System32", "cmd.exe"), nil
}

func termEnv(u *user.User, home, shell string) []string {
	env := []string{"TERM=xterm-256color", "COLORTERM=truecolor"}
	for _, k := range []string{"SYSTEMROOT", "SYSTEMDRIVE", "WINDIR", "PATH", "USERPROFILE", "APPDATA", "LOCALAPPDATA",
		"PROGRAMDATA", "PROGRAMFILES", "TEMP", "TMP", "COMSPEC", "PATHEXT", "USERNAME", "USERDOMAIN", "COMPUTERNAME", "TZ"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	if os.Getenv("USERPROFILE") == "" {
		env = append(env, "USERPROFILE="+home)
	}
	if os.Getenv("USERNAME") == "" && u != nil {
		env = append(env, "USERNAME="+u.Username)
	}
	return env
}
