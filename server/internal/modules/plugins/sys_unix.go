//go:build unix

package plugins

import (
	"io/fs"
	"os"
	"os/user"
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

// socketOwnedByUs reports whether the socket at p belongs to this user.
func socketOwnedByUs(_ string, fi fs.FileInfo) bool {
	uid, ok := fileUID(fi)
	return ok && uid == os.Geteuid()
}

// platformCaller fills in the calling user and groups; root, and members of
// sudo, wheel or admin (who can unlock administrator rights), are admins.
func platformCaller(c *caller) {
	if os.Geteuid() == 0 {
		c.Admin = true
	}
	u, err := user.Current()
	if err != nil {
		return
	}
	c.Name = u.Username
	ids, _ := u.GroupIds()
	for _, id := range ids {
		if g, err := user.LookupGroupId(id); err == nil {
			c.Groups[g.Name] = true
		}
	}
	for _, g := range []string{"sudo", "wheel", "admin"} {
		if c.Groups[g] {
			c.Admin = true // can unlock administrator rights
		}
	}
}
