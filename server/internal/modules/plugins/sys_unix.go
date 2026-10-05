//go:build unix

package plugins

import (
	"io/fs"
	"os"
	"syscall"
)

// fileUID returns the owner of fi.
func fileUID(fi fs.FileInfo) (int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}

// renameAt renames oldName to newName, both inside the open folder dir.
func renameAt(dir *os.File, oldName, newName string) error {
	fd := int(dir.Fd())
	return syscall.Renameat(fd, oldName, fd, newName)
}
