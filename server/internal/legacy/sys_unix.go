//go:build unix

package legacy

import (
	"io/fs"
	"syscall"
)

const oNoFollow = syscall.O_NOFOLLOW

// fileOwner returns the uid and gid of fi.
func fileOwner(fi fs.FileInfo) (uid, gid int, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}
