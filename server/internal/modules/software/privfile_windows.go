//go:build windows

package software

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"

	"github.com/ervisio/ervisio/server/internal/winsec"
)

// No O_NOFOLLOW/O_NONBLOCK on windows: links are refused by inspecting the
// object without following it (winsec.InspectPath).
const (
	oNoFollow = 0
	oNonblock = 0
)

// fileOwnerUID has no uid on windows; -1 never matches a trusted owner.
func fileOwnerUID(os.FileInfo) int { return -1 }

// Windows has no uid and no mode bits: ownership and "nobody else can write"
// are read from the owner SID and the DACL. Root is SYSTEM or
// Administrators; "us" adds the account of this process.

// trusted returns the SIDs that may own the object: root (SYSTEM,
// Administrators) and, unless rootOnly, this process's user.
func trusted(rootOnly bool) ([]*windows.SID, error) {
	set := []*windows.SID{winsec.System(), winsec.Administrators()}
	if !rootOnly {
		me, err := winsec.ProcessUser()
		if err != nil {
			return nil, err
		}
		set = append(set, me)
	}
	return set, nil
}

// inspect opens path without following a link and checks it is a plain
// folder or file (not a reparse point) owned by one of owners; it returns
// the entries of its DACL.
func inspect(path string, wantDir bool, owners []*windows.SID) ([]winsec.Ace, error) {
	in, err := winsec.InspectPath(path)
	if err != nil {
		return nil, err
	}
	if in.Reparse {
		return nil, fmt.Errorf("%s is a link", path)
	}
	if in.Dir != wantDir {
		return nil, fmt.Errorf("%s is not the expected kind of object", path)
	}
	if !winsec.SIDIn(in.Owner, owners...) {
		return nil, fmt.Errorf("%s belongs to %s", path, in.Owner)
	}
	if in.NoDACL {
		return nil, fmt.Errorf("%s has no access list: everyone can write", path)
	}
	return winsec.Aces(in.DACL)
}

// noOtherWriters fails when an allow entry gives write access to a SID
// outside allowed.
func noOtherWriters(path string, aces []winsec.Ace, allowed []*windows.SID) error {
	for _, a := range aces {
		if a.SID == nil || !a.Allow || a.Mask&winsec.WriteMask == 0 || winsec.SIDIn(a.SID, allowed...) {
			continue
		}
		return fmt.Errorf("%s can be written by %s (mask %#x)", path, a.SID, a.Mask)
	}
	return nil
}

// dirTrust: dir is a plain folder owned by SYSTEM, Administrators or this
// user, and nobody else can write to it.
func dirTrust(dir string, _ os.FileInfo, _ []int) error {
	owners, err := trusted(false)
	if err != nil {
		return err
	}
	aces, err := inspect(dir, true, owners)
	if err != nil {
		return err
	}
	return noOtherWriters(dir, aces, owners)
}

// privateDir: dir is owned by this user (or Administrators/SYSTEM, which own
// what an elevated process creates) and only they can write to it. With
// create the folder gets a protected DACL of SYSTEM, Administrators and this
// user.
func privateDir(dir string, _ os.FileInfo, create bool) error {
	owners, err := trusted(false)
	if err != nil {
		return err
	}
	if create {
		if _, err := inspect(dir, true, owners); err != nil { // owner and kind before changing the ACL
			return err
		}
		var es []winsec.Entry
		for _, s := range owners {
			es = append(es, winsec.Entry{SID: s, Mask: winsec.FileAllAccess, Inherit: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT})
		}
		if err := winsec.SetProtectedDACL(dir, es); err != nil {
			return err
		}
	}
	aces, err := inspect(dir, true, owners)
	if err != nil {
		return err
	}
	return noOtherWriters(dir, aces, owners)
}

// fileTrust: the plain file at path is owned by root (owner 0) or, for any
// other owner, by root or this user.
func fileTrust(path string, _ os.FileInfo, owner int) error {
	owners, err := trusted(owner == 0)
	if err != nil {
		return err
	}
	_, err = inspect(path, false, owners)
	return err
}

// pidRunning reports whether a process with this pid exists and has not exited.
func pidRunning(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	return windows.GetExitCodeProcess(h, &code) == nil && code == 259 // STILL_ACTIVE
}
