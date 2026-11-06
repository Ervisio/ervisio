package config

import (
	"os"

	"github.com/ervisio/ervisio/server/internal/winsec"
	"golang.org/x/sys/windows"
)

// Chmod does not implement Unix read bits on Windows. Configuration published
// as 0644 must remain readable by filtered user bridges after atomic saves.
// Private 0600 files retain their existing private directory inheritance.
func readableFileACL(path string, mode os.FileMode) error {
	if mode.Perm()&0004 == 0 {
		return nil
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	return winsec.SetProtectedDACL(path, []winsec.Entry{
		{SID: user.User.Sid, Mask: winsec.FileAllAccess},
		{SID: winsec.System(), Mask: winsec.FileAllAccess},
		{SID: winsec.Administrators(), Mask: winsec.FileAllAccess},
		{SID: winsec.AuthenticatedUsers(), Mask: windows.FILE_GENERIC_READ},
	})
}
