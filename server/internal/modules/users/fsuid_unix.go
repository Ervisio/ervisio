//go:build unix

package users

import (
	"os"
	"runtime"
	"syscall"
)

// openNoFollow makes open fail on a symlink as the last path element.
const openNoFollow = syscall.O_NOFOLLOW

// asUser runs fn with the file-system identity of uid/gid when the bridge runs
// as root, so a user-controlled symlink can never make us touch anything the
// user could not. As a normal user it just runs fn.
func asUser(uid, gid int, fn func() error) error {
	if os.Geteuid() != 0 || uid == 0 {
		return fn()
	}
	runtime.LockOSThread()
	if err := syscall.Setfsgid(gid); err != nil {
		runtime.UnlockOSThread()
		return err
	}
	if err := syscall.Setfsuid(uid); err != nil {
		_ = syscall.Setfsgid(0)
		runtime.UnlockOSThread()
		return err
	}
	err := fn()
	e1 := syscall.Setfsuid(0)
	e2 := syscall.Setfsgid(0)
	if e1 == nil && e2 == nil {
		runtime.UnlockOSThread()
	} // else: leave the thread locked so it ends with this goroutine
	return err
}
