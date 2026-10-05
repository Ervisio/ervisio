//go:build unix

package envs

import (
	"os"
	"syscall"
)

// fileOwner returns the owner of fi (ok is false when it is not known).
func fileOwner(fi os.FileInfo) (uid, gid int, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}
