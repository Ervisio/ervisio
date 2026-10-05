//go:build unix

package logs

import (
	"os"
	"syscall"
)

// openShared opens a file for reading.
func openShared(path string) (*os.File, error) { return os.Open(path) }

// fileInode returns the inode of an Stat result.
func fileInode(_ string, info os.FileInfo) (uint64, bool) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino), true
	}
	return 0, false
}
