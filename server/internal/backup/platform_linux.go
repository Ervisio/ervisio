//go:build linux

package backup

import (
	"errors"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func privateOwner(fi os.FileInfo) bool {
	s, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(s.Uid) == os.Geteuid() && fi.Mode().Perm()&0077 == 0
}

func openProtected(p string, flags int, mode os.FileMode) (*os.File, error) {
	fd, err := unix.Open(p, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, uint32(mode.Perm()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), p), nil
}

func protectedReadFlags() int { return os.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK }

// Open every component relative to an already opened directory descriptor.
// O_NOFOLLOW on only the final file would still follow a replaced ancestor.
func openArchiveRead(root *os.Root, rel string, directory bool) (*os.File, error) {
	current, err := root.OpenFile(".", protectedReadFlags()|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return current, nil
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			current.Close()
			return nil, errors.New("Unsafe archive path component.")
		}
		flags := protectedReadFlags() | unix.O_CLOEXEC
		if i < len(parts)-1 || directory {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(int(current.Fd()), part, flags, 0)
		current.Close()
		if err != nil {
			return nil, err
		}
		current = os.NewFile(uintptr(fd), part)
	}
	return current, nil
}

func availableBytes(p string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(p, &st); err != nil {
		return 0, err
	}
	if st.Bsize <= 0 || st.Bavail > uint64((1<<63-1)/st.Bsize) {
		return 0, errors.New("The spool free-space report is invalid.")
	}
	return int64(st.Bavail) * st.Bsize, nil
}

func lockFile(p string) (func(), error) {
	f, err := openProtected(p, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || !privateOwner(fi) {
		f.Close()
		return nil, errors.New("The backup lock is not private.")
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, errBusy
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
