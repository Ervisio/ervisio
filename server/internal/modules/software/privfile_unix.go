//go:build unix

package software

import (
	"fmt"
	"os"
	"syscall"
)

const (
	oNoFollow = syscall.O_NOFOLLOW
	oNonblock = syscall.O_NONBLOCK
)

// fileOwnerUID returns the owner uid of an Lstat result, or -1.
func fileOwnerUID(fi os.FileInfo) int {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid)
	}
	return -1
}

// dirTrust: dir (an Lstat result, a real directory) is owned by one of
// owners and nobody else can write to it.
func dirTrust(dir string, fi os.FileInfo, owners []int) error {
	uid := ownerOf(fi)
	ok := false
	for _, o := range owners {
		ok = ok || uid == o
	}
	if !ok {
		return fmt.Errorf("%s belongs to uid %d", dir, uid)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s can be written by other users (mode %04o)", dir, fi.Mode().Perm())
	}
	return nil
}

// privateDir: dir is owned by the current user with mode 0700; with create a
// too open mode is fixed (dir is ours and its parent only ours or root's: no
// link can be swapped in).
func privateDir(dir string, fi os.FileInfo, create bool) error {
	if ownerOf(fi) != os.Geteuid() {
		return fmt.Errorf("%s belongs to uid %d", dir, ownerOf(fi))
	}
	if fi.Mode().Perm() != 0o700 {
		if !create {
			return fmt.Errorf("%s has mode %04o, not 0700", dir, fi.Mode().Perm())
		}
		return os.Chmod(dir, 0o700)
	}
	return nil
}

// fileTrust: the regular file at path belongs to uid owner.
func fileTrust(path string, fi os.FileInfo, owner int) error {
	if ownerOf(fi) != owner {
		return fmt.Errorf("%s belongs to uid %d, not %d", path, ownerOf(fi), owner)
	}
	return nil
}

// pidRunning reports whether a process with this pid exists.
func pidRunning(pid int) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}
