//go:build windows

package account

import (
	"fmt"
	"os/user"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// sidAdministrators is the BUILTIN\Administrators group.
const sidAdministrators = "S-1-5-32-544"

// nameRe is a Windows account name: "user", "DOMAIN\user" or "user@domain",
// letters/digits plus space, dot, dash, underscore and "$" (machine
// accounts). It cannot start with a space or end with one or a dot.
var nameRe = regexp.MustCompile(`^[\p{L}\p{N}_$][\p{L}\p{N}_.@ \\$-]{0,126}[\p{L}\p{N}_$-]$|^[\p{L}\p{N}_$]$`)

// ridOf returns the last sub-authority of a SID string.
func ridOf(sid string) (uint32, error) {
	i := strings.LastIndexByte(sid, '-')
	if !strings.HasPrefix(sid, "S-") || i < 0 {
		return 0, fmt.Errorf("sid %q: malformed", sid)
	}
	n, err := strconv.ParseUint(sid[i+1:], 10, 32)
	if err != nil {
		return 0, fmt.Errorf("sid %q: %w", sid, err)
	}
	return uint32(n), nil
}

// Current resolves the user running this process.
func Current() (*Account, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	return fromUser(u)
}

// fromUser maps an os/user result (SIDs for Uid/Gid/GroupIds on Windows).
func fromUser(u *user.User) (*Account, error) {
	uid, err := ridOf(u.Uid)
	if err != nil {
		return nil, err
	}
	gid, err := ridOf(u.Gid)
	if err != nil {
		return nil, err
	}
	a := &Account{
		Name:     u.Username,
		FullName: strings.TrimSpace(u.Name),
		UID:      uid,
		GID:      gid,
		SID:      u.Uid,
		Home:     u.HomeDir,
	}
	ids, err := u.GroupIds()
	if err != nil {
		ids = []string{u.Gid}
	}
	for _, id := range ids {
		n, err := ridOf(id)
		if err != nil {
			continue
		}
		a.Groups = append(a.Groups, n)
		a.GroupSIDs = append(a.GroupSIDs, id)
		if g, err := user.LookupGroupId(id); err == nil {
			a.GroupNames = append(a.GroupNames, g.Name)
		}
	}
	if !slices.Contains(a.Groups, a.GID) {
		a.Groups = append(a.Groups, a.GID)
	}
	return a, nil
}

// IsRoot is always false: Windows has no uid 0. (UID holds a RID, and RID 500,
// the built-in Administrator, is an ordinary account for the allow_root rule.)
func (a *Account) IsRoot() bool { return false }

// CanSudo is a hint for the UI: membership of BUILTIN\Administrators.
func (a *Account) CanSudo() bool {
	return slices.Contains(a.GroupSIDs, sidAdministrators)
}

// ShellsFile is unused on Windows; it exists so shared code compiles.
var ShellsFile = ""

// ShellAllowed is always true on Windows: accounts have no login shell, so
// the nologin/restricted-shell rule has nothing to check.
func ShellAllowed(shell string) bool { return true }
