//go:build windows

package sshauth

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/ervisio/ervisio/server/internal/winsec"
)

var errUnsupported = errors.New("not supported on windows")

// adminKeysFile is OpenSSH for Windows' administrators_authorized_keys.
func adminKeysFile() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		return ""
	}
	return filepath.Join(pd, "ssh", "administrators_authorized_keys")
}

// extraKeyFiles: members of Administrators also use
// %ProgramData%\ssh\administrators_authorized_keys (OpenSSH for Windows).
func extraKeyFiles(u User) []string {
	if f := adminKeysFile(); u.Admin && f != "" {
		return []string{f}
	}
	return nil
}

// readSecure opens p without following a reparse point and checks, on the
// open handle: a regular file, not a reparse point; owned by the user, SYSTEM
// or Administrators (the administrators file: SYSTEM or Administrators
// only); a DACL that grants Everyone, Authenticated Users or Users no write
// access (a missing DACL grants everyone everything and is refused). The
// daemon runs as SYSTEM, so no identity switch is needed.
func readSecure(p string, u User) ([]byte, error) {
	h, err := winsec.Open(p, windows.GENERIC_READ)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), p) // owns h from here
	defer f.Close()
	in, err := winsec.Inspect(h)
	if err != nil {
		return nil, err
	}
	if err := checkKeyFile(in, p, u); err != nil {
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

func checkKeyFile(in *winsec.Info, p string, u User) error {
	if in.Reparse {
		return errors.New("is a reparse point (symbolic link or junction)")
	}
	if in.Dir {
		return errors.New("not a regular file")
	}
	trusted := []*windows.SID{winsec.System(), winsec.Administrators()}
	if af := adminKeysFile(); af == "" || !strings.EqualFold(filepath.Clean(p), af) {
		if u.SID == "" {
			return errors.New("the account has no SID, so the owner of the key file cannot be checked")
		}
		usid, err := windows.StringToSid(u.SID)
		if err != nil {
			return fmt.Errorf("account SID %q: %w", u.SID, err)
		}
		trusted = append(trusted, usid)
	}
	if !winsec.SIDIn(in.Owner, trusted...) {
		return fmt.Errorf("bad owner %s", in.Owner)
	}
	if in.NoDACL {
		return errors.New("no DACL (everyone has full access)")
	}
	aces, err := winsec.Aces(in.DACL)
	if err != nil {
		return err
	}
	bad := []*windows.SID{winsec.Everyone(), winsec.AuthenticatedUsers(), winsec.Users()}
	for _, a := range aces {
		if a.Allow && a.SID != nil && a.Mask&winsec.WriteMask != 0 && winsec.SIDIn(a.SID, bad...) {
			return fmt.Errorf("writable by %s (access mask %#x)", a.SID, a.Mask)
		}
	}
	return nil
}

// switchFS: there is no file-system identity to switch on Windows (asUser
// never calls it: os.Geteuid is -1).
func switchFS(u User) (restore func() error, err error) {
	return func() error { return nil }, errUnsupported
}
