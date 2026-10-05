//go:build unix

package envs

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

func prepareTunnelDirFor(base, dir, _ string) error { return prepareTunnelDir(base, dir) }

func listenOwnedFor(base, path string, uid, gid int, _ string) (net.Listener, error) {
	return listenOwned(base, path, uid, gid)
}

// tunnelDirMode is the mode of the tunnel folders: root's, traversable.
const tunnelDirMode = 0o711

// ownedByUs reports whether fi is owned by the daemon's user (root in
// production).
func ownedByUs(fi os.FileInfo) bool {
	uid, _, ok := fileOwner(fi)
	return ok && uid == os.Geteuid()
}

// prepareTunnelDir makes base and dir folders of the daemon's user with
// mode 0711, nothing else: not a symlink, not the user's. A folder left
// from an older version (the user's own, 0700) is removed with whatever
// the user put in it (os.RemoveAll does not follow symlinks) and made
// again. base is made by the daemon only, so nobody else can create or
// rename entries in it.
func prepareTunnelDir(base, dir string) error {
	if err := os.MkdirAll(base, tunnelDirMode); err != nil {
		return err
	}
	bi, err := os.Lstat(base)
	if err != nil {
		return err
	}
	if !bi.IsDir() || !ownedByUs(bi) {
		return fmt.Errorf("%s is not a folder of this service", base)
	}
	if err := os.Chmod(base, tunnelDirMode); err != nil {
		return err
	}
	fi, err := os.Lstat(dir)
	if err == nil && (!fi.IsDir() || !ownedByUs(fi) || fi.Mode().Perm() != tunnelDirMode) {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		fi, err = nil, os.ErrNotExist
	}
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(dir, tunnelDirMode); err != nil {
			return err
		}
		if err := os.Chmod(dir, tunnelDirMode); err != nil { // umask
			return err
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() || !ownedByUs(fi) || fi.Mode().Perm() != tunnelDirMode {
		return fmt.Errorf("%s is not a folder of this service", dir)
	}
	return nil
}

// listenOwned listens on a unix socket at path that belongs to uid:gid with
// mode 0600. The socket is bound in a new folder of base that only the
// daemon can enter, given its owner and mode there, and only then renamed to
// path, so nobody can swap it for a symlink while the daemon changes it.
// The result is checked again with Lstat.
func listenOwned(base, path string, uid, gid int) (net.Listener, error) {
	tmp, err := os.MkdirTemp(base, ".new-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o700); err != nil {
		return nil, err
	}
	staged := filepath.Join(tmp, "s")
	ln, err := net.Listen("unix", staged)
	if err != nil {
		return nil, err
	}
	// The socket file moves; tunnel.close removes it at its final path.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	fail := func(err error) (net.Listener, error) {
		ln.Close()
		return nil, err
	}
	if err := os.Chmod(staged, 0o600); err != nil {
		return fail(err)
	}
	if os.Geteuid() == 0 {
		if err := os.Lchown(staged, uid, gid); err != nil {
			return fail(err)
		}
	}
	// A socket left by an earlier run (the daemon's folder: no one else's).
	if fi, err := os.Lstat(path); err == nil && !fi.IsDir() {
		_ = os.Remove(path)
	}
	if err := os.Rename(staged, path); err != nil {
		return fail(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return fail(err)
	}
	fuid, fgid, ok := fileOwner(fi)
	if fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 || !ok ||
		(os.Geteuid() == 0 && (fuid != uid || fgid != gid)) {
		_ = os.Remove(path)
		return fail(fmt.Errorf("the socket %s did not get the expected owner and mode", path))
	}
	return ln, nil
}
