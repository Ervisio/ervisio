//go:build windows

package server

import (
	"github.com/ervisio/ervisio/server/internal/winsec"
	"golang.org/x/sys/windows"
	"os"
	"unsafe"
)

// Apply a protected DACL at creation, before another account can open it.
func createStateDirectory(path string) error {
	me, err := winsec.ProcessUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + me.String() + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	if err = windows.CreateDirectory(name, &sa); err != nil {
		return &os.PathError{Op: "mkdir", Path: path, Err: err}
	}
	return nil
}

// Windows state directories inherit the daemon's private ProgramData ACL.
// Individual preference files use config.WriteAtomic's private DACL.
func stateOwnedBySelf(path string, info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	entry, err := winsec.InspectPath(path)
	if err != nil || entry.Reparse || entry.NoDACL {
		return false
	}
	me, err := winsec.ProcessUser()
	if err != nil {
		return false
	}
	if !winsec.SIDIn(entry.Owner, me, winsec.System(), winsec.Administrators()) {
		return false
	}
	aces, err := winsec.Aces(entry.DACL)
	if err != nil {
		return false
	}
	for _, ace := range aces {
		if ace.SID == nil {
			return false
		}
		if ace.Allow && ace.Mask&winsec.WriteMask != 0 && !winsec.SIDIn(ace.SID, me, winsec.System(), winsec.Administrators()) {
			return false
		}
	}
	return true
}
