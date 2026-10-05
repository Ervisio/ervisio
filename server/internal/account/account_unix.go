//go:build !windows

package account

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// AdminGroups are the groups whose members are usually allowed to sudo.
var AdminGroups = []string{"wheel", "sudo", "admin"}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@-]{0,63}\$?$`)

// Current resolves the user running this process.
func Current() (*Account, error) {
	u, err := user.LookupId(strconv.Itoa(os.Getuid()))
	if err != nil {
		return nil, err
	}
	return fromUser(u)
}

func fromUser(u *user.User) (*Account, error) {
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("uid %q: %w", u.Uid, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("gid %q: %w", u.Gid, err)
	}
	a := &Account{
		Name:     u.Username,
		FullName: strings.TrimSpace(strings.Split(u.Name, ",")[0]),
		UID:      uint32(uid),
		GID:      uint32(gid),
		Home:     u.HomeDir,
		Shell:    loginShell(u.Username),
	}
	ids, err := u.GroupIds()
	if err != nil {
		ids = []string{u.Gid}
	}
	for _, id := range ids {
		n, err := strconv.ParseUint(id, 10, 32)
		if err != nil {
			continue
		}
		a.Groups = append(a.Groups, uint32(n))
		if g, err := user.LookupGroupId(id); err == nil {
			a.GroupNames = append(a.GroupNames, g.Name)
		}
	}
	if !slices.Contains(a.Groups, a.GID) {
		a.Groups = append(a.Groups, a.GID)
	}
	if a.Home == "" {
		a.Home = "/"
	}
	return a, nil
}

// IsRoot reports whether the account has uid 0.
func (a *Account) IsRoot() bool { return a.UID == 0 }

// CanSudo is a hint for the UI: root, or member of an admin group.
// sudo itself has the final word when unlocking.
func (a *Account) CanSudo() bool {
	if a.IsRoot() {
		return true
	}
	for _, g := range a.GroupNames {
		if slices.Contains(AdminGroups, g) {
			return true
		}
	}
	return false
}

// loginShell returns the account's login shell as NSS reports it
// (/etc/passwd, LDAP/sssd, systemd-homed…), or "" when it is unknown. There
// is deliberately no fallback: an account without a known shell cannot sign
// in (see ShellAllowed).
func loginShell(name string) string { return nssShell(name) }

// ShellsFile is the list of valid login shells (variable for tests).
var ShellsFile = "/etc/shells"

// restrictedShells are listed in /etc/shells on some systems but mean "no
// interactive login": a web console with file access and a terminal would
// bypass the restriction they impose.
var restrictedShells = map[string]bool{
	"nologin": true, "false": true, "true": true, "git-shell": true, "rbash": true, "rksh": true,
	"rssh": true, "scponly": true, "sftp-server": true, "internal-sftp": true,
}

// ShellAllowed reports whether an account with this login shell may sign
// in: the shell must be an absolute path listed in /etc/shells and must not
// be a "no login" or restricted shell (nologin, false, git-shell, rbash…).
// When /etc/shells is missing, /bin/sh and /bin/bash style defaults apply as
// in getusershell(3).
func ShellAllowed(shell string) bool {
	if shell == "" || !strings.HasPrefix(shell, "/") || strings.ContainsAny(shell, "\x00\n") {
		return false
	}
	if restrictedShells[filepath.Base(shell)] {
		return false
	}
	data, err := os.ReadFile(ShellsFile)
	if err != nil {
		return shell == "/bin/sh" || shell == "/bin/csh"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == shell {
			return true
		}
	}
	return false
}
