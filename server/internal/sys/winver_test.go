package sys

import "testing"

func TestWindowsRelease(t *testing.T) {
	r := WindowsRelease("Windows 10 Pro", "23H2", "22631")
	if r.ID != "windows" || r.Name != "Windows 11 Pro" || r.PrettyName != "Windows 11 Pro 23H2" || r.VersionID != "23H2" {
		t.Fatalf("%+v", r)
	}
	r = WindowsRelease("Windows Server 2022 Standard", "", "20348")
	if r.Name != "Windows Server 2022 Standard" || r.PrettyName != r.Name {
		t.Fatalf("%+v", r)
	}
	if WindowsRelease("", "", "").Name != "Windows" {
		t.Fatal("default name")
	}
	if WindowsRelease("Windows 10 Home", "22H2", "19045").Name != "Windows 10 Home" {
		t.Fatal("win10 kept")
	}
}

func TestWindowsLogo(t *testing.T) {
	for build, want := range map[string]string{"26100": "windows-11", "22631": "windows-11", "20348": "windows-10", "19045": "windows-10", "14393": "windows-10", "9600": "windows-8", "7601": "windows-7", "": "windows-11"} {
		if got := WindowsRelease("Windows", "", build).Logo; got != want {
			t.Errorf("build %q: logo %q, want %q", build, got, want)
		}
	}
}
