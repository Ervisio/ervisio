//go:build windows

package users

import "errors"

// openNoFollow has no open flag on windows. It is only used by the key file
// helpers, which run inside asUser, and asUser refuses to run on windows.
const openNoFollow = 0

// asUser would have to run fn as the account uid/gid, which is not
// implemented on windows yet. It refuses rather than touch the files with the
// identity of the bridge.
func asUser(uid, gid int, fn func() error) error {
	return errors.New("per-user file access is not supported on windows yet")
}
