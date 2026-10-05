//go:build windows

// Package winsec holds the small file-security helpers the Windows ports of
// the SSH-key and tunnel checks share: opening without following reparse
// points, reading the owner and DACL of an open handle, listing ACEs and
// setting a protected DACL.
package winsec

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Info is the security-relevant state of an open file or folder.
type Info struct {
	Reparse bool
	Dir     bool
	Owner   *windows.SID
	// DACL is nil when the object has no DACL or an empty one; NoDACL tells
	// "no DACL" (everyone has full access) from an empty one (nobody has).
	DACL   *windows.ACL
	NoDACL bool
	sd     *windows.SECURITY_DESCRIPTOR // keeps Owner and DACL alive
}

// Open opens path for READ_CONTROL plus access without following a reparse
// point as its last component (FILE_FLAG_OPEN_REPARSE_POINT), sharing it
// with everyone. Folders can be opened too (FILE_FLAG_BACKUP_SEMANTICS).
func Open(path string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, access|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
}

// Inspect reads the attributes, owner and DACL of an open handle.
func Inspect(h windows.Handle) (*Info, error) {
	var fi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &fi); err != nil {
		return nil, err
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return nil, err
	}
	in := &Info{
		Reparse: fi.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0,
		Dir:     fi.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0,
		sd:      sd,
	}
	if in.Owner, _, err = sd.Owner(); err != nil || in.Owner == nil {
		return nil, errors.New("no owner")
	}
	dacl, _, err := sd.DACL()
	switch {
	case errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND):
		in.NoDACL = true
	case err != nil:
		return nil, err
	default:
		in.DACL = dacl
	}
	return in, nil
}

// InspectPath opens path (not following a final reparse point) and inspects it.
func InspectPath(path string) (*Info, error) {
	h, err := Open(path, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	return Inspect(h)
}

// Ace is one access control entry. SID is nil for ACE types other than the
// plain and callback allow/deny entries.
type Ace struct {
	Type  uint8
	Mask  uint32
	SID   *windows.SID
	Allow bool
}

// Aces lists the entries of acl.
func Aces(acl *windows.ACL) ([]Ace, error) {
	if acl == nil {
		return nil, nil
	}
	out := make([]Ace, 0, acl.AceCount)
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var p *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &p); err != nil {
			return nil, err
		}
		a := Ace{Type: p.Header.AceType}
		switch a.Type {
		case 0, 9: // ACCESS_ALLOWED, ACCESS_ALLOWED_CALLBACK: same layout
			a.Allow = true
			fallthrough
		case 1, 10: // ACCESS_DENIED, ACCESS_DENIED_CALLBACK
			a.Mask = uint32(p.Mask)
			a.SID = (*windows.SID)(unsafe.Pointer(&p.SidStart))
		}
		out = append(out, a)
	}
	return out, nil
}

// Well-known SIDs.
func wk(t windows.WELL_KNOWN_SID_TYPE) *windows.SID {
	s, err := windows.CreateWellKnownSid(t)
	if err != nil {
		panic(err) // cannot fail for these types
	}
	return s
}

func System() *windows.SID         { return wk(windows.WinLocalSystemSid) }
func Administrators() *windows.SID { return wk(windows.WinBuiltinAdministratorsSid) }
func Everyone() *windows.SID       { return wk(windows.WinWorldSid) }
func AuthenticatedUsers() *windows.SID {
	return wk(windows.WinAuthenticatedUserSid)
}
func Users() *windows.SID { return wk(windows.WinBuiltinUsersSid) }

// SIDIn reports whether s equals one of set.
func SIDIn(s *windows.SID, set ...*windows.SID) bool {
	for _, x := range set {
		if x != nil && s.Equals(x) {
			return true
		}
	}
	return false
}

// Write-like access bits: FILE_WRITE_DATA, FILE_APPEND_DATA, FILE_WRITE_EA,
// DELETE, WRITE_DAC, WRITE_OWNER, GENERIC_WRITE, GENERIC_ALL.
const WriteMask = 0x2 | 0x4 | 0x10 | 0x10000 | 0x40000 | 0x80000 | 0x40000000 | 0x10000000

// Entry is one ACE of a DACL to set.
type Entry struct {
	SID     *windows.SID
	Mask    uint32
	Inherit uint32 // windows.NO_INHERITANCE, SUB_CONTAINERS_AND_OBJECTS_INHERIT...
}

// SetProtectedDACL replaces the DACL of path with exactly entries and stops
// it inheriting from the parent.
func SetProtectedDACL(path string, entries []Entry) error {
	ea := make([]windows.EXPLICIT_ACCESS, len(entries))
	for i, e := range entries {
		ea[i] = windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.ACCESS_MASK(e.Mask),
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       e.Inherit,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
				TrusteeValue: windows.TrusteeValueFromSID(e.SID),
			},
		}
	}
	acl, err := windows.ACLFromEntries(ea, nil)
	if err != nil {
		return err
	}
	// Through a handle opened without following a reparse point, so a socket
	// file (an AF_UNIX reparse point) can be changed too.
	h, err := Open(path, windows.WRITE_DAC)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

// FileAllAccess is FILE_ALL_ACCESS.
const FileAllAccess = 0x001F01FF

// ProcessUser returns the SID of the user this process runs as.
func ProcessUser() (*windows.SID, error) {
	t := windows.GetCurrentProcessToken()
	u, err := t.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid, nil
}
