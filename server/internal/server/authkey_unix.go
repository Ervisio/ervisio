//go:build unix

package server

import (
	"fmt"
	"os"
	"syscall"
)

const (
	oNoFollow = syscall.O_NOFOLLOW
	oNonBlock = syscall.O_NONBLOCK
)

// checkDevAuthorizedKeys checks the --dev-authorized-keys file: a regular
// file (not a symlink) owned by the daemon's user and not writable by group
// or others, so another local account cannot add its own key to it.
func checkDevAuthorizedKeys(f *os.File) error {
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		return err
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return fmt.Errorf("--dev-authorized-keys %s: not a regular file", f.Name())
	}
	if int(st.Uid) != os.Geteuid() || st.Mode&0o022 != 0 {
		return fmt.Errorf("--dev-authorized-keys %s: must be owned by uid %d and not writable by group or others (owner uid %d, mode %04o)",
			f.Name(), os.Geteuid(), st.Uid, st.Mode&0o7777)
	}
	return nil
}

// keyLogonToken: a key sign-in needs no token on unix (the bridge starts
// with setuid).
func (s *Server) keyLogonToken(string) error { return nil }

// openDevAuthorizedKeys opens the file without following a final symlink.
func openDevAuthorizedKeys(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|oNoFollow|oNonBlock, 0)
}
