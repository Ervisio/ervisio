// Package account resolves a Linux user name to the identity the daemon
// needs to start a bridge for it.
package account

import (
	"fmt"
	"os/user"
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
	// SID is the account's security identifier on Windows ("" elsewhere);
	// UID and GID are then its RID and its primary group's RID.
	SID string
	// GroupSIDs are the SIDs of GroupNames on Windows ("" elsewhere).
	GroupSIDs []string
}

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

// SameUser reports whether a and b are the same account. When both carry a
// SID (Windows, where UID is only a RID and can collide across domains) the
// SIDs are compared; otherwise the UIDs.
func SameUser(a, b *Account) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.SID != "" && b.SID != "" {
		return strings.EqualFold(a.SID, b.SID)
	}
	return a.UID == b.UID
}

// LostGroup reports the first group of old that cur no longer has: by SID when
// (by SID when both carry GroupSIDs, else by gid), printable;
// "" when none is lost.
func LostGroup(old, cur *Account) string {
	if len(old.GroupSIDs) > 0 && len(cur.GroupSIDs) > 0 {
		for _, g := range old.GroupSIDs {
			if !slices.ContainsFunc(cur.GroupSIDs, func(c string) bool { return strings.EqualFold(c, g) }) {
				return g
			}
		}
		return ""
	}
	for _, g := range old.Groups {
		if !slices.Contains(cur.Groups, g) {
			return strconv.FormatUint(uint64(g), 10)
		}
	}
	return ""
}
