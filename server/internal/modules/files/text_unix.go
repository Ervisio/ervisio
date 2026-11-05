//go:build unix

package files

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
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
		target, isSymlink, err := finalLinkTarget(path)
		if err != nil {
			return "", err
		}
		if !isSymlink {
			return path, nil
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		path = filepath.Clean(target)
	}
	return "", pathErr("open", path, unix.ELOOP)
}

// finalLinkTarget reads the link at path when it is one owned by root. A
// missing or non-link path is not an error: isSymlink is false.
func finalLinkTarget(path string) (target string, isSymlink bool, err error) {
	d, name, err := openParent(path)
	if err != nil {
		return "", false, err
	}
	fd, err := unix.Openat(dfd(d), name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	d.Close()
	if errors.Is(err, unix.ENOENT) {
		return "", false, nil
	}
	if err != nil {
		return "", false, pathErr("open", path, err)
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return "", false, pathErr("stat", path, err)
	}
	if !isLink(&st) {
		return "", false, nil
	}
	if st.Uid != trustedLinkUID {
		return "", false, errUntrustedLink(path)
	}
	target, err = readlinkAt(fd, "")
	if err != nil {
		return "", false, pathErr("readlink", path, err)
	}
	return target, true, nil
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
	st, exists, err := statForSave(dfd(d), name, path, expected)
	if err != nil {
		return nil, err
	}
	terr := saveViaTemp(d, name, path, data, &st, exists)
	if terr == nil {
		return saved(path)
	}
	// The folder may not be writable although the file is: write in place.
	if exists && os.IsPermission(terr) {
		return saveInPlace(d, name, path, data)
	}
	return nil, terr
}

// statForSave looks at the file to be saved and checks it is still what the
// editor opened.
func statForSave(dirfd int, name, path string, expected int64) (unix.Stat_t, bool, error) {
	st, err := lstatAt(dirfd, name)
	if errors.Is(err, unix.ENOENT) {
		return st, false, checkExpectedAbsent(expected)
	}
	if err != nil {
		return st, false, pathErr("lstat", path, err)
	}
	if !isReg(&st) {
		return st, true, errNotRegularText()
	}
	return st, true, checkExpectedMtime(time.Unix(st.Mtim.Unix()).UnixMilli(), expected)
}

// saveViaTemp writes data to a temp file next to name and renames it over name.
func saveViaTemp(d *os.File, name, path string, data []byte, st *unix.Stat_t, exists bool) error {
	tmp, tname, err := createTempAt(dfd(d), ".save-", filepath.Dir(path))
	if err != nil {
		return err
	}
	err = fillTemp(tmp, data, st, exists)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		// renameat replaces whatever is at name now, never what a link points to.
		if err = unix.Renameat(dfd(d), tname, dfd(d), name); err == nil {
			return nil
		}
		err = pathErr("rename", path, err)
	}
	_ = unix.Unlinkat(dfd(d), tname, 0)
	return err
}

// fillTemp writes data into tmp and gives it the owner and mode of the file it replaces.
func fillTemp(tmp *os.File, data []byte, st *unix.Stat_t, exists bool) error {
	tfd := int(tmp.Fd())
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	perm := uint32(0o644)
	if exists {
		perm = st.Mode & 0o7777
		if os.Geteuid() == 0 {
			_ = unix.Fchown(tfd, int(st.Uid), int(st.Gid)) // before chmod: chown clears setuid bits
		}
	}
	return unix.Fchmod(tfd, perm)
}

func saveInPlace(d *os.File, name, path string, data []byte) (any, error) {
	fd, err := unix.Openat(dfd(d), name, unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, pathErr("open", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	var now unix.Stat_t
	if err := unix.Fstat(fd, &now); err != nil || !isReg(&now) {
		f.Close()
		return nil, errNotRegularText()
	}
	return finishInPlaceSave(f, path, data, unix.Ftruncate(fd, 0))
}
