//go:build windows

package main

import "golang.org/x/sys/windows"

// selfUID is the uid the bridge reports in its hello: the RID of the user
// SID in its own token, which is what the daemon's account.Account.UID
// holds. It reads the token rather than os/user, because user.Current needs
// a loaded profile, which the bridge does not have.
func selfUID() int {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return -1
	}
	sid := tu.User.Sid
	n := sid.SubAuthorityCount()
	if n == 0 {
		return -1
	}
	return int(sid.SubAuthority(uint32(n - 1)))
}
