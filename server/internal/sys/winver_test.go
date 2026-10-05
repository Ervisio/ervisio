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
