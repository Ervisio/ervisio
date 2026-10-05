//go:build unix

package files

import (
	"os"
	"syscall"
)

// setOwner fills the owner fields of e from the file system record.
func setOwner(e *Entry, fi os.FileInfo) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		e.UID, e.GID = int(st.Uid), int(st.Gid)
		e.Owner, e.Group = lookupUser(e.UID), lookupGroup(e.GID)
	}
}

// setOwnerPath is used on Windows, where owners are looked up by path.
func setOwnerPath(e *Entry, full string, fi os.FileInfo) {}
