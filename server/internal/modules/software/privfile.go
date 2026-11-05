package software

// Private directories and state files that other local users cannot plant,
// swap or read (docs/SECURITY-REVIEW.md M3 and L1).

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ervisio/ervisio/server/internal/config"
)

// ownerOf returns the owner uid of an Lstat result (-1 on Windows).
func ownerOf(fi os.FileInfo) int { return fileOwnerUID(fi) }

// checkTrustedDir verifies that dir is a real directory (not a link) owned by
// one of owners that no one else can write to.
func checkTrustedDir(dir string, owners ...int) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return dirTrust(dir, fi, owners)
}

// ensurePrivateDir makes sure dir is a directory owned by the current user
// with mode 0700. Its parent must be a directory only this user (or root) can
// write to, so no one else can have planted dir. With create, the parent is
// created when missing, dir is created, and a too open mode is fixed.
func ensurePrivateDir(dir string, create bool) error {
	me := os.Geteuid()
	parent := filepath.Dir(dir)
	if create {
		perm := os.FileMode(0o700)
		if me == 0 {
			perm = 0o755
		}
		if err := os.MkdirAll(parent, perm); err != nil {
			return err
		}
	}
	if err := checkTrustedDir(parent, me, 0); err != nil {
		return err
	}
	fi, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return privateDir(dir, fi, create)
}

// writeFileAtomic replaces path with data and mode: a new file is created with
// O_EXCL (never through a link) next to it and renamed over it. The directory
// must be owned by the current user and not writable by others.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := checkTrustedDir(dir, os.Geteuid()); err != nil {
		return err
	}
	return config.WriteAtomic(path, data, mode)
}

const maxStateFile = 4 << 20

// readOwnedFile reads a regular file that must belong to owner, refusing links
// (O_NOFOLLOW) and anything else planted under that name.
func readOwnedFile(path string, owner int) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|oNoFollow|oNonblock, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if err := fileTrust(path, fi, owner); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, maxStateFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxStateFile {
		return nil, fmt.Errorf("%s is too large", path)
	}
	return b, nil
}
