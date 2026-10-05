//go:build unix

package files

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// resolveFinal follows the links at the end of path, so saving a link saves
// the file it points to. In the root bridge only links owned by root are
// followed; each hop is resolved through directory descriptors.
func resolveFinal(path string) (string, error) {
	if !strictLinks {
		if r, err := filepath.EvalSymlinks(path); err == nil {
			return r, nil
		}
		return path, nil
	}
	for i := 0; i < maxLinkHops; i++ {
		d, name, err := openParent(path)
		if err != nil {
			return "", err
		}
		fd, err := unix.Openat(dfd(d), name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		d.Close()
		if errors.Is(err, unix.ENOENT) {
			return path, nil
		}
		if err != nil {
			return "", pathErr("open", path, err)
		}
		var st unix.Stat_t
		err = unix.Fstat(fd, &st)
		if err != nil || !isLink(&st) {
			unix.Close(fd)
			return path, pathErr("stat", path, err)
		}
		if st.Uid != trustedLinkUID {
			unix.Close(fd)
			return "", errUntrustedLink(path)
		}
		t, err := readlinkAt(fd, "")
		unix.Close(fd)
		if err != nil {
			return "", pathErr("readlink", path, err)
		}
		if !filepath.IsAbs(t) {
			t = filepath.Join(filepath.Dir(path), t)
		}
		path = filepath.Clean(t)
	}
	return "", pathErr("open", path, unix.ELOOP)
}

func writeTextFile(path string, data []byte, expected int64) (any, error) {
	path, err := resolveFinal(path)
	if err != nil {
		return nil, err
	}
	d, name, err := openParent(path)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	st, err := lstatAt(dfd(d), name)
	exists := err == nil
	if err != nil && !errors.Is(err, unix.ENOENT) {
		return nil, pathErr("lstat", path, err)
	}
	if exists {
		if !isReg(&st) {
			return nil, rpc.Errorf(rpc.Invalid, "Only regular files can be saved as text.")
		}
		mt := time.Unix(st.Mtim.Unix()).UnixMilli()
		if expected != 0 && mt != expected {
			return nil, rpc.Errorf(rpc.Conflict, "This file was changed by someone else since you opened it. Reload it, or copy your text first.").
				WithData(map[string]any{"mtime": mt})
		}
	} else if expected != 0 {
		return nil, rpc.Errorf(rpc.Conflict, "This file was removed since you opened it.")
	}
	perm := uint32(0o644)
	if exists {
		perm = st.Mode & 0o7777
	}
	tmp, tname, terr := createTempAt(dfd(d), ".save-", filepath.Dir(path))
	if terr == nil {
		tfd := int(tmp.Fd())
		_, werr := tmp.Write(data)
		if werr == nil && exists && os.Geteuid() == 0 {
			_ = unix.Fchown(tfd, int(st.Uid), int(st.Gid)) // before chmod: chown clears setuid bits
		}
		if werr == nil {
			werr = unix.Fchmod(tfd, perm)
		}
		cerr := tmp.Close()
		if werr == nil && cerr == nil {
			// renameat replaces whatever is at name now, never what a link points to.
			rerr := unix.Renameat(dfd(d), tname, dfd(d), name)
			if rerr == nil {
				return saved(path)
			}
			werr = pathErr("rename", path, rerr)
		}
		_ = unix.Unlinkat(dfd(d), tname, 0)
		if werr == nil {
			werr = cerr
		}
		terr = werr
	}
	// The folder may not be writable although the file is: write in place.
	if exists && os.IsPermission(terr) {
		fd, err := unix.Openat(dfd(d), name, unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, pathErr("open", path, err)
		}
		f := os.NewFile(uintptr(fd), path)
		var now unix.Stat_t
		if err := unix.Fstat(fd, &now); err != nil || !isReg(&now) {
			f.Close()
			return nil, rpc.Errorf(rpc.Invalid, "Only regular files can be saved as text.")
		}
		err = unix.Ftruncate(fd, 0)
		if err == nil {
			_, err = f.Write(data)
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, pathErr("write", path, err)
		}
		return saved(path)
	}
	return nil, terr
}
