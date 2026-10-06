//go:build windows

package server

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"

	"github.com/ervisio/ervisio/server/internal/pam"
	"github.com/ervisio/ervisio/server/internal/winsec"
)

// openDevAuthorizedKeys opens the file itself, not what a symlink or junction
// points to (FILE_FLAG_OPEN_REPARSE_POINT), so checkDevAuthorizedKeys sees
// the reparse point and refuses it.
func openDevAuthorizedKeys(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// checkDevAuthorizedKeys checks the --dev-authorized-keys file on its open
// handle: a regular file owned by the daemon's user, SYSTEM or
// Administrators, with a DACL that gives Everyone, Authenticated Users and
// Users no write access.
func checkDevAuthorizedKeys(f *os.File) error {
	in, err := winsec.Inspect(windows.Handle(f.Fd()))
	if err != nil {
		return err
	}
	if in.Dir || in.Reparse { // a symlink or junction is refused, as O_NOFOLLOW does on unix
		return fmt.Errorf("--dev-authorized-keys %s: not a regular file", f.Name())
	}
	me, err := winsec.ProcessUser()
	if err != nil {
		return err
	}
	if !winsec.SIDIn(in.Owner, me, winsec.System(), winsec.Administrators()) {
		return fmt.Errorf("--dev-authorized-keys %s: owned by %s, not by the daemon's user", f.Name(), in.Owner)
	}
	if in.NoDACL {
		return fmt.Errorf("--dev-authorized-keys %s: no DACL", f.Name())
	}
	aces, err := winsec.Aces(in.DACL)
	if err != nil {
		return err
	}
	for _, a := range aces {
		if a.Allow && a.SID != nil && a.Mask&winsec.WriteMask != 0 &&
			winsec.SIDIn(a.SID, winsec.Everyone(), winsec.AuthenticatedUsers(), winsec.Users()) {
			return fmt.Errorf("--dev-authorized-keys %s: writable by %s", f.Name(), a.SID)
		}
	}
	return nil
}

// keyLogonToken gets the user's token without a password (S4U logon, see
// pam.AuthenticateS4U) so the bridge can start as the user.
func (s *Server) keyLogonToken(name string) error {
	if s.opts.Dev { // --dev does not switch user: no token needed
		return nil
	}
	return pam.AuthenticateS4U(name)
}
