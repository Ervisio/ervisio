// Package account resolves a Linux user name to the identity the daemon
// needs to start a bridge for it.
package account

import (
	"fmt"
	"os"
	"os/user"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Account is a resolved local user.
type Account struct {
	Name       string
	FullName   string
	UID        uint32
	GID        uint32
	Groups     []uint32 // supplementary group ids (including GID)
	GroupNames []string
	Home       string
	Shell      string
}

// AdminGroups are the groups whose members are usually allowed to sudo.
var AdminGroups = []string{"wheel", "sudo", "admin"}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@-]{0,63}\$?$`)

// ValidName reports whether s is an acceptable login name.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// Lookup resolves name via NSS.
func Lookup(name string) (*Account, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid user name")
	}
	u, err := user.Lookup(name)
	if err != nil {
		return nil, err
	}
	return fromUser(u)
}

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

// loginShell reads the shell from /etc/passwd (os/user does not expose it).
func loginShell(name string) string {
	data, err := os.ReadFile("/etc/passwd")
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Split(line, ":")
			if len(f) == 7 && f[0] == name && f[6] != "" {
				return f[6]
			}
		}
	}
	return "/bin/sh"
}
