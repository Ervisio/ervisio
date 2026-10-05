//go:build unix

package overview

import (
	"os"
	"syscall"
)

// diskUsage returns the used and available bytes of the filesystem at point.
func diskUsage(point string) (used, avail uint64, ok bool) {
	var st syscall.Statfs_t
	if syscall.Statfs(point, &st) != nil || st.Blocks == 0 {
		return 0, 0, false
	}
	bs := uint64(st.Bsize)
	return (st.Blocks - st.Bfree) * bs, st.Bavail * bs, true
}

// fileOwner returns the owner uid of an Stat result.
func fileOwner(fi os.FileInfo) (uint32, bool) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Uid, true
	}
	return 0, false
}
