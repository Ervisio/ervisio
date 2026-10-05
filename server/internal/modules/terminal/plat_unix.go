//go:build !windows

package terminal

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

func defaultHome() string { return "/" }

// shellArgv: leading dash makes a login shell.
func shellArgv(base string) []string { return []string{"-" + base} }

func lookSSH() (string, error) { return sys.LookPath("ssh") }

// resolveShell picks the requested shell (must be listed in /etc/shells) or
// the login shell from /etc/passwd, falling back to /bin/sh.
func resolveShell(want, username string) (string, error) {
	if want != "" {
		if !filepath.IsAbs(want) || !shellListed(want) {
			return "", rpc.Errorf(rpc.Invalid, "%s is not an allowed shell. Pick one listed in /etc/shells.", want)
		}
		return want, nil
	}
	if sh := passwdShell(username); sh != "" {
		return sh, nil
	}
	return "/bin/sh", nil
}

func shellListed(path string) bool {
	f, err := os.Open("/etc/shells")
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == path {
			fi, err := os.Stat(path)
			return err == nil && fi.Mode()&0o111 != 0
		}
	}
	return false
}

func passwdShell(username string) string {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	return parsePasswdShell(f, username)
}

// systemLang reads LANG from the environment or /etc/locale.conf.
func systemLang() string {
	if v := os.Getenv("LANG"); v != "" && v != "C" {
		return v
	}
	if f, err := os.Open("/etc/locale.conf"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "LANG="); ok {
				v = strings.Trim(v, `"'`)
				if v != "" {
					return v
				}
			}
		}
	}
	return "C.UTF-8"
}

func termEnv(u *user.User, home, shell string) []string {
	env := []string{
		"PATH=" + sys.SafePath,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=" + systemLang(),
		"HOME=" + home,
		"USER=" + u.Username,
		"LOGNAME=" + u.Username,
	}
	if shell != "" {
		env = append(env, "SHELL="+shell)
	}
	if v := os.Getenv("TZ"); v != "" {
		env = append(env, "TZ="+v)
	}
	rt := fmt.Sprintf("/run/user/%s", u.Uid)
	if fi, err := os.Stat(rt); err == nil && fi.IsDir() {
		env = append(env, "XDG_RUNTIME_DIR="+rt)
	}
	return env
}
