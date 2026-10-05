//go:build unix

package system

import (
	"fmt"
	"syscall"
	"time"
)

// diskUsage returns the size, used and available bytes of the file system
// mounted at path.
func diskUsage(path string) (total, used, avail uint64, err error) {
	var st syscall.Statfs_t
	if err := statfsTimeout(path, &st); err != nil {
		return 0, 0, 0, err
	}
	bs := uint64(st.Bsize)
	total = st.Blocks * bs
	used = (st.Blocks - st.Bfree) * bs
	avail = st.Bavail * bs
	return total, used, avail, nil
}

// statfsTimeout guards against hung network filesystems.
func statfsTimeout(path string, st *syscall.Statfs_t) error {
	done := make(chan error, 1)
	var local syscall.Statfs_t
	go func() { done <- syscall.Statfs(path, &local) }()
	select {
	case err := <-done:
		*st = local
		return err
	case <-time.After(time.Second):
		return fmt.Errorf("statfs %s: timeout", path)
	}
}
