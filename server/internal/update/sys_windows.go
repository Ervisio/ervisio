//go:build windows

package update

import (
	"os"

	"golang.org/x/sys/windows"
)

// Windows has no O_NOFOLLOW or O_NONBLOCK for os.OpenFile.
const (
	oNoFollow = 0
	oNonBlock = 0
)

func lockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
}

func unlockFile(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}

// checkOwner cannot verify the owner on Windows: it only accepts files when
// no owner is required.
func checkOwner(fi os.FileInfo, owner int) bool { return owner < 0 }
