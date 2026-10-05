//go:build linux

package sshauth

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// switchFS changes the calling thread's file-system identity to u and
// returns the function that puts root's back (nil error when it did).
func switchFS(u User) (restore func() error, err error) {
	oldGroups, err := unix.Getgroups()
	if err != nil {
		return func() error { return nil }, err
	}
	restore = func() error {
		e1 := unix.Setfsuid(0)
		e2 := unix.Setfsgid(0)
		e3 := unix.Setgroups(oldGroups)
		uid, _ := unix.SetfsuidRetUid(-1)
		gid, _ := unix.SetfsgidRetGid(-1)
		if e := errors.Join(e1, e2, e3); e != nil {
			return e
		}
		if uid != 0 || gid != 0 {
			return errors.New("file-system identity not restored")
		}
		return nil
	}
	groups := make([]int, 0, len(u.Groups)+1)
	groups = append(groups, int(u.GID))
	for _, g := range u.Groups {
		if g != u.GID {
			groups = append(groups, int(g))
		}
	}
	if err := unix.Setgroups(groups); err != nil {
		return restore, fmt.Errorf("setgroups: %w", err)
	}
	if err := unix.Setfsgid(int(u.GID)); err != nil {
		return restore, err
	}
	if err := unix.Setfsuid(int(u.UID)); err != nil {
		return restore, err
	}
	uid, _ := unix.SetfsuidRetUid(-1)
	gid, _ := unix.SetfsgidRetGid(-1)
	if uid != int(u.UID) || gid != int(u.GID) {
		return restore, fmt.Errorf("cannot take the file-system identity of uid %d (now fsuid %d, fsgid %d)", u.UID, uid, gid)
	}
	return restore, nil
}
