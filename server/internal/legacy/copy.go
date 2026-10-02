package legacy

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// copyTree copies src into dst (which must not exist): folders, regular
// files and symlinks, with their permission bits and, when running as root,
// their owners. Other file types are skipped. skip, when set, is called
// with each path relative to src and leaves it (and its contents) out.
func copyTree(src, dst string, skip func(rel string) bool) error {
	// Folder modes are set at the end: a read-only folder must stay
	// writable while its contents are copied.
	dirModes := map[string]fs.FileMode{}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel != "." && skip != nil && skip(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case fi.IsDir():
			if err := os.Mkdir(target, 0o700); err != nil {
				return err
			}
			lchown(target, fi)
			dirModes[target] = fi.Mode().Perm() | fi.Mode()&(fs.ModeSetgid|fs.ModeSticky)
			return nil
		case fi.Mode()&fs.ModeSymlink != 0:
			t, err := os.Readlink(p)
			if err != nil {
				return err
			}
			if err := os.Symlink(t, target); err != nil {
				return err
			}
			lchown(target, fi)
			return nil
		case fi.Mode().IsRegular():
			if err := copyFile(p, target, fi.Mode().Perm()); err != nil {
				return err
			}
		default:
			return nil // sockets, FIFOs, devices: not ours to copy
		}
		lchown(target, fi)
		return os.Chmod(target, fi.Mode().Perm())
	})
	if err != nil {
		return err
	}
	for d, m := range dirModes {
		if err := os.Chmod(d, m); err != nil {
			return err
		}
	}
	return nil
}

// lchown gives target the owner of fi when running as root.
func lchown(target string, fi fs.FileInfo) {
	if os.Geteuid() != 0 {
		return
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		_ = os.Lchown(target, int(st.Uid), int(st.Gid))
	}
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// installTree copies src to dst through a temporary sibling folder renamed
// into place at the end, so dst is either complete or absent. An existing
// dst is replaced only when replace allows it. edit, when set, may change
// the copy before it is renamed.
func installTree(src, dst string, skip func(rel string) bool, edit func(tmp string) error) error {
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(parent, "."+filepath.Base(dst)+".import-"+randHex(6))
	err := func() error {
		if err := copyTree(src, tmp, skip); err != nil {
			return err
		}
		if edit != nil {
			if err := edit(tmp); err != nil {
				return err
			}
		}
		if _, err := os.Lstat(dst); err == nil {
			old := filepath.Join(parent, "."+filepath.Base(dst)+".replaced-"+randHex(6))
			if err := os.Rename(dst, old); err != nil {
				return err
			}
			defer os.RemoveAll(old)
		}
		if err := os.Rename(tmp, dst); err != nil {
			return err
		}
		return syncDir(parent)
	}()
	if err != nil {
		os.RemoveAll(tmp)
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	return nil
}

// removeStale deletes leftovers of interrupted installTree calls for dst.
func removeStale(dst string) {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".import-*"))
	more, _ := filepath.Glob(filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".replaced-*"))
	for _, m := range append(matches, more...) {
		os.RemoveAll(m)
	}
}

// hasFiles reports whether dir holds any file other than the import marker
// (folders alone do not count: packages create empty ones).
func hasFiles(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipDir
		}
		if !d.IsDir() && !(filepath.Dir(p) == dir && d.Name() == importMarker) {
			found = true
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

// writeFileAtomic writes data to path through a temporary file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp-" + randHex(4)
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func isDir(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.IsDir()
}

func isRegular(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().IsRegular()
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func notExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	_ = d.Sync()
	return nil
}
