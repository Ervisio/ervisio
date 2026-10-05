//go:build windows

package legacy

import "testing"

// LinuxAdmin never ran on Windows: every entry point is a no-op or refuses.
func TestNothingToMigrateOnWindows(t *testing.T) {
	if Installed(Paths{}) {
		t.Fatal("Installed on Windows")
	}
	if res, err := ImportOnStart(Paths{}, nil); err != nil || res.Any() {
		t.Fatalf("%v %v", res, err)
	}
	if _, ok := LegacyVersionOf(Paths{}, `C:\Program Files\Ervisio\bin\ervisiod.exe`); ok {
		t.Fatal("legacy layout on Windows")
	}
	if ok, err := MigrateUserDir(t.TempDir()); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if RunMigrate(MigrateFlag, nil) == 0 {
		t.Fatal("migrate must refuse on Windows")
	}
}
