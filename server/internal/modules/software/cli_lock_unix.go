//go:build !windows

package software

import (
	"os"
	"syscall"
)

func lockCLIFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
func unlockCLIFile(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
