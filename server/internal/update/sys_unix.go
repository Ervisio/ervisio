//go:build unix

package update

import (
	"os"
	"syscall"
)

const (
	oNoFollow = syscall.O_NOFOLLOW
	oNonBlock = syscall.O_NONBLOCK
)

// lockFile takes an exclusive, non-blocking lock on f; unlockFile releases it.
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlockFile(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

// checkOwner reports whether fi is owned by owner (any owner when negative).
func checkOwner(fi os.FileInfo, owner int) bool {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && owner >= 0 && int(st.Uid) != owner {
		return false
	}
	return true
}
