package users

// Platform-neutral mapping of the raw NetUser*/NetLocalGroup* records (read
// with netapi32 in winnative_windows.go) onto the winSnapshot shapes.

import (
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

const (
	ufPasswdNotReqd  = 0x0020
	ufAccountDisable = 0x0002
	netForever       = 0xFFFFFFFF // TIMEQ_FOREVER
	nerrAccessDenied = 5          // ERROR_ACCESS_DENIED
)

// nativeUser is the subset of USER_INFO_3 that the mapping needs.
type nativeUser struct {
	Name        string
	FullName    string
	Comment     string
	Flags       uint32
	PasswordAge uint32 // seconds since the password was set
	LastLogon   uint32 // unix seconds, 0 = never
	AcctExpires uint32 // unix seconds, netForever = never
}

func (n nativeUser) toWinUser(sid, profile string, now time.Time) winUser {
	u := winUser{
		Name: n.Name, SID: sid, FullName: n.FullName, Description: n.Comment,
		Enabled:         n.Flags&ufAccountDisable == 0,
		PasswordRequire: n.Flags&ufPasswdNotReqd == 0,
		LastLogon:       int64(n.LastLogon),
		Profile:         profile,
	}
	if n.PasswordAge > 0 {
		u.PasswordLastSet = now.Unix() - int64(n.PasswordAge)
		if u.PasswordLastSet < 0 {
			u.PasswordLastSet = 0
		}
	}
	if n.AcctExpires != netForever {
		u.AccountExpires = int64(n.AcctExpires)
	}
	return u
}

// netStatusError maps a NET_API_STATUS to an rpc error.
func netStatusError(what string, status uint32, detail string) error {
	if status == nerrAccessDenied {
		return rpc.Errorf(rpc.NeedsAdmin, "Administrator rights are needed: %s", what)
	}
	return rpc.Errorf(rpc.Internal, "%s: %s (error %d)", what, detail, status)
}
