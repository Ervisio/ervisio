//go:build unix

package logs

import (
	"os"
	"syscall"
)

// fileInode returns the inode of an Stat result.
func fileInode(info os.FileInfo) (uint64, bool) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino), true
	}
	return 0, false
}
