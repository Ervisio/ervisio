// Package account resolves a Linux user name to the identity the daemon
// needs to start a bridge for it.
package account

import (
	"fmt"
	"os/user"
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
