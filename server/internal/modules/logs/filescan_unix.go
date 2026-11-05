//go:build unix

package logs

import (
	"os"
	"syscall"
)

func openShared(path string) (*os.File, error) { return os.Open(path) }

// fileInode reports the inode, and false when the platform stat has none.
func fileInode(_ string, info os.FileInfo) (uint64, bool) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino), true
	}
	return 0, false
}
