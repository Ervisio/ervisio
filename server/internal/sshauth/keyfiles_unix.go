//go:build unix

package sshauth

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// extraKeyFiles are the platform's additional key files (none on unix).
func extraKeyFiles(User) []string { return nil }

func ownerOK(st *syscall.Stat_t, uid uint32) bool { return st.Uid == 0 || st.Uid == uid }

// readSecure opens path (symlinks resolved, like sshd), checks it and
// its directories, and reads it.
func readSecure(p string, u User) ([]byte, error) {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(real, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		return nil, err
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return nil, errors.New("not a regular file")
	}
	if !ownerOK(&st, u.UID) || st.Mode&0o022 != 0 {
		return nil, fmt.Errorf("bad ownership or modes (owner uid %d, mode %04o)", st.Uid, st.Mode&0o7777)
	}
	if err := checkDirs(filepath.Dir(real), u); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, maxKeysFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxKeysFile {
		return nil, errors.New("larger than 1 MiB")
	}
	return b, nil
}

// checkDirs walks from dir up to the home directory (included) or /, as
// sshd's auth_secure_path does.
func checkDirs(dir string, u User) error {
	home, err := filepath.EvalSymlinks(u.Home)
	if err != nil {
		home = filepath.Clean(u.Home)
	}
	for {
		var st syscall.Stat_t
		if err := syscall.Stat(dir, &st); err != nil {
			return err
		}
		if !ownerOK(&st, u.UID) || st.Mode&0o022 != 0 {
			return fmt.Errorf("bad ownership or modes for directory %s (owner uid %d, mode %04o)", dir, st.Uid, st.Mode&0o7777)
		}
		if dir == home || dir == "/" {
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}
