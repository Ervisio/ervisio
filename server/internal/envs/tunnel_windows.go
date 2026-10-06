//go:build windows

package envs

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"github.com/ervisio/ervisio/server/internal/winsec"
)

// Windows tunnels are AF_UNIX sockets (Windows 10 1803 and later). Access is
// by ACL instead of uid/mode:
//
//	<tunnel dir>                 SYSTEM + the daemon's user + Administrators:
//	                             full; Authenticated Users: traverse only
//	<tunnel dir>\<rid>           protected DACL: SYSTEM + the daemon's user:
//	                             full; the account's SID: read and traverse
//	<tunnel dir>\<rid>\<id>.sock SYSTEM + daemon's user: full; the account's
//	                             SID: read and write
//
// Every object is checked on a handle opened without following reparse
// points (the socket is itself one, tag AF_UNIX): owner SYSTEM, the daemon's
// user or Administrators, and no ACE for anyone else.

const (
	fileTraverse  = windows.FILE_GENERIC_EXECUTE
	fileRead      = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_EXECUTE
	fileReadWrite = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE
)

// trustedOwners are the SIDs that may own and fully control the folders.
func trustedOwners() ([]*windows.SID, error) {
	me, err := winsec.ProcessUser()
	if err != nil {
		return nil, err
	}
	return []*windows.SID{winsec.System(), me, winsec.Administrators()}, nil
}

// ours returns SYSTEM and the daemon's user (one entry when they are the same).
func ours() ([]*windows.SID, error) {
	me, err := winsec.ProcessUser()
	if err != nil {
		return nil, err
	}
	if me.Equals(winsec.System()) {
		return []*windows.SID{me}, nil
	}
	return []*windows.SID{winsec.System(), me}, nil
}

func fullFor(sids []*windows.SID) []winsec.Entry {
	var es []winsec.Entry
	for _, s := range sids {
		es = append(es, winsec.Entry{SID: s, Mask: winsec.FileAllAccess})
	}
	return es
}

// checkObject verifies path (not followed if a reparse point): folder or not
// as wanted, owned by a trusted SID, and every access-allowed entry for a SID
// outside allow has no write-like access (strict: no entry at all for
// strict). Deny entries are ignored.
func checkObject(path string, wantDir, strict bool, allow []*windows.SID) error {
	in, err := winsec.InspectPath(path)
	if err != nil {
		return err
	}
	if wantDir && (!in.Dir || in.Reparse) {
		return errors.New("not a plain folder")
	}
	if !wantDir && in.Dir {
		return errors.New("is a folder")
	}
	owners, err := trustedOwners()
	if err != nil {
		return err
	}
	if !winsec.SIDIn(in.Owner, owners...) {
		return fmt.Errorf("owned by %s", in.Owner)
	}
	if in.NoDACL {
		return errors.New("no DACL")
	}
	aces, err := winsec.Aces(in.DACL)
	if err != nil {
		return err
	}
	for _, a := range aces {
		if a.SID == nil {
			return fmt.Errorf("unexpected ACE type %d", a.Type)
		}
		if !a.Allow || winsec.SIDIn(a.SID, allow...) {
			continue
		}
		if strict || a.Mask&winsec.WriteMask != 0 {
			return fmt.Errorf("access granted to %s (mask %#x)", a.SID, a.Mask)
		}
	}
	return nil
}

// prepareTunnelDir makes base and dir for the daemon alone (SYSTEM only
// beyond the daemon's user), see prepareTunnelDirFor.
func prepareTunnelDir(base, dir string) error { return prepareTunnelDirFor(base, dir, "") }

// prepareTunnelDirFor makes base and dir folders of this service with the
// ACLs described above: not reparse points, not the user's. A folder that
// fails the check is removed with whatever it holds (os.RemoveAll does not
// follow links) and made again.
func prepareTunnelDirFor(base, dir, sid string) error {
	me, err := ours()
	if err != nil {
		return err
	}
	owners, err := trustedOwners()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}
	baseACL := append(fullFor(owners), winsec.Entry{SID: winsec.AuthenticatedUsers(), Mask: fileTraverse})
	// Owned by this service (its parent's ACL may have been inherited, so
	// the DACL is not judged yet), then given its own DACL and checked.
	in, err := winsec.InspectPath(base)
	if err != nil {
		return err
	}
	if !in.Dir || in.Reparse || !winsec.SIDIn(in.Owner, owners...) {
		return fmt.Errorf("%s is not a folder of this service", base)
	}
	if err := winsec.SetProtectedDACL(base, baseACL); err != nil {
		return err
	}
	if err := checkObject(base, true, false, owners); err != nil {
		return fmt.Errorf("%s is not a folder of this service: %v", base, err)
	}

	allow, entries := me, fullFor(me)
	if sid != "" {
		us, err := windows.StringToSid(sid)
		if err != nil {
			return fmt.Errorf("account SID %q: %w", sid, err)
		}
		allow = append(append([]*windows.SID{}, me...), us)
		entries = append(entries, winsec.Entry{SID: us, Mask: fileRead})
	}
	if _, err := os.Lstat(dir); err == nil {
		if checkObject(dir, true, true, allow) != nil {
			if err := removeTree(dir, me); err != nil {
				return err
			}
		}
	}
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
	}
	if err := winsec.SetProtectedDACL(dir, entries); err != nil {
		return err
	}
	if err := checkObject(dir, true, true, allow); err != nil {
		return fmt.Errorf("%s is not a folder of this service: %v", dir, err)
	}
	return nil
}

// removeTree removes a folder tree. A folder left by an older version or by
// the user can deny this service every access (its DACL was emptied when the
// parent's was replaced, or it is the user's own), so it cannot be opened,
// listed or deleted. The removal is retried after taking ownership (with the
// take-ownership, backup and restore privileges an administrator holds) and
// giving this service full access, inherited by what the folder holds.
// os.RemoveAll does not follow links.
func removeTree(dir string, me []*windows.SID) error {
	err := os.RemoveAll(dir)
	if err == nil {
		return nil
	}
	// Best effort: a token without these still gets the plain attempts below.
	_ = winsec.EnablePrivileges("SeTakeOwnershipPrivilege", "SeRestorePrivilege", "SeBackupPrivilege")
	var es []winsec.Entry
	for _, s := range me {
		es = append(es, winsec.Entry{SID: s, Mask: winsec.FileAllAccess, Inherit: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT | windows.OBJECT_INHERIT_ACE})
	}
	owner := me[len(me)-1]
	if e := winsec.TakeOwnership(dir, owner, es); e != nil {
		// Without the privilege: the DACL alone, as the owner.
		if e2 := winsec.SetProtectedDACL(dir, es); e2 != nil {
			return fmt.Errorf("%w (taking ownership: %v)", err, e)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return nil
}

// listenOwned is listenOwnedFor without an account (reachable by the
// daemon's side only).
func listenOwned(base, path string, uid, gid int) (net.Listener, error) {
	return listenOwnedFor(base, path, uid, gid, "")
}

// listenOwnedFor listens on a unix socket at path that sid may open. The
// socket is bound in a new folder of base that only the daemon can enter,
// given its DACL there, and only then renamed to path, so nobody can swap it
// for a link while its ACL is set. The result is checked again.
func listenOwnedFor(base, path string, _, _ int, sid string) (net.Listener, error) {
	me, err := ours()
	if err != nil {
		return nil, err
	}
	allow, entries := me, fullFor(me)
	if sid != "" {
		us, err := windows.StringToSid(sid)
		if err != nil {
			return nil, fmt.Errorf("account SID %q: %w", sid, err)
		}
		allow = append(append([]*windows.SID{}, me...), us)
		entries = append(entries, winsec.Entry{SID: us, Mask: fileReadWrite})
	}
	tmp, err := os.MkdirTemp(base, ".new-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := winsec.SetProtectedDACL(tmp, fullFor(me)); err != nil {
		return nil, err
	}
	staged := filepath.Join(tmp, "s")
	ln, err := net.Listen("unix", staged)
	if err != nil {
		return nil, err
	}
	// The socket file moves; tunnel.close removes it at its final path.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	fail := func(err error) (net.Listener, error) {
		ln.Close()
		return nil, err
	}
	if err := winsec.SetProtectedDACL(staged, entries); err != nil {
		return fail(err)
	}
	// A socket left by an earlier run (the daemon's folder: no one else's).
	if fi, err := os.Lstat(path); err == nil && !fi.IsDir() {
		_ = os.Remove(path)
	}
	if err := os.Rename(staged, path); err != nil {
		return fail(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return fail(err)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		_ = os.Remove(path)
		return fail(fmt.Errorf("%s is not a socket", path))
	}
	if err := checkObject(path, false, true, allow); err != nil {
		_ = os.Remove(path)
		return fail(fmt.Errorf("the socket %s did not get the expected owner and ACL: %v", path, err))
	}
	return ln, nil
}
