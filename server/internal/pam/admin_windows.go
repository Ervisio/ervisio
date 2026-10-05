//go:build windows

package pam

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrNotAdmin means the account is not a member of BUILTIN\Administrators,
// so it has no elevated token to unlock (the Windows "sudo refused").
var ErrNotAdmin = errors.New("account is not an administrator")

// Token elevation types (TOKEN_ELEVATION_TYPE).
const (
	elevationDefault = 1
	elevationFull    = 2
	elevationLimited = 3
)

// hasAdminGroup reports whether t carries an enabled BUILTIN\Administrators
// SID (a filtered UAC token has it deny-only, which does not count).
func hasAdminGroup(t windows.Token) (bool, error) {
	groups, err := t.GetTokenGroups()
	if err != nil {
		return false, err
	}
	admins, err := windows.StringToSid("S-1-5-32-544")
	if err != nil {
		return false, err
	}
	for _, g := range groups.AllGroups() {
		if g.Attributes&windows.SE_GROUP_ENABLED != 0 && windows.EqualSid(g.Sid, admins) {
			return true, nil
		}
	}
	return false, nil
}

// AdminToken returns a new elevated primary token for user, derived from the
// token kept by Authenticate (call Authenticate with the password first).
// A filtered UAC token (elevation type Limited) yields its linked token; a
// token that is already full yields a duplicate of itself. Users outside
// Administrators get ErrNotAdmin. The caller owns the returned token and must
// close it (windows.Token(t).Close()) once the process using it is gone.
func AdminToken(user string) (windows.Token, error) {
	kept, ok := lookupToken(user)
	if !ok {
		return 0, errors.New("pam: no logon token for this account: sign in again")
	}
	var etype, n uint32
	if err := windows.GetTokenInformation(kept, windows.TokenElevationType, (*byte)(unsafe.Pointer(&etype)), uint32(unsafe.Sizeof(etype)), &n); err != nil {
		return 0, err
	}
	src := kept
	switch etype {
	case elevationLimited:
		linked, err := kept.GetLinkedToken()
		if err != nil {
			return 0, ErrNotAdmin // no linked token: a standard user
		}
		defer linked.Close()
		src = linked
	case elevationDefault, elevationFull:
	default:
		return 0, errors.New("pam: unknown token elevation type")
	}
	admin, err := hasAdminGroup(src)
	if err != nil {
		return 0, err
	}
	if !admin {
		return 0, ErrNotAdmin
	}
	var primary windows.Token
	if err := windows.DuplicateTokenEx(src, windows.MAXIMUM_ALLOWED, nil, windows.SecurityImpersonation, windows.TokenPrimary, &primary); err != nil {
		return 0, err
	}
	return primary, nil
}
