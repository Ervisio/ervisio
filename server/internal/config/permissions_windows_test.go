package config

import (
	"path/filepath"
	"testing"

	"github.com/ervisio/ervisio/server/internal/winsec"
	"golang.org/x/sys/windows"
)

func TestAtomicConfigSavesKeepUserReadWithoutSharingPrivateBackups(t *testing.T) {
	dir := t.TempDir()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if err = winsec.SetProtectedDACL(dir, []winsec.Entry{
		{SID: user.User.Sid, Mask: winsec.FileAllAccess, Inherit: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT},
		{SID: winsec.System(), Mask: winsec.FileAllAccess, Inherit: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT},
		{SID: winsec.Administrators(), Mask: winsec.FileAllAccess, Inherit: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT},
	}); err != nil {
		t.Fatal(err)
	}
	check := func(path string, readable bool) {
		t.Helper()
		info, err := winsec.InspectPath(path)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := winsec.Aces(info.DACL)
		if err != nil {
			t.Fatal(err)
		}
		read := false
		for _, entry := range entries {
			if entry.Allow && winsec.SIDIn(entry.SID, winsec.AuthenticatedUsers()) {
				if entry.Mask&winsec.WriteMask != 0 {
					t.Fatal("Authenticated Users may write the configuration")
				}
				read = entry.Mask&windows.FILE_READ_DATA != 0
			}
		}
		if read != readable {
			t.Fatalf("public read=%v, wanted %v for %s", read, readable, path)
		}
	}
	path := filepath.Join(dir, "ervisio.conf")
	cfg := Default()
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	check(path, true)
	cfg.UI.DefaultShell = "ervisios"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	check(path, true)
	check(path+".bak", false)
}
