//go:build !windows

package users

import (
	"os"
	"path/filepath"
	"testing"
)

// The SSH key methods are Unix-only (users_windows.go lists them as
// unsupported), and so is the file layout they check.

func TestKeyFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ssh", "authorized_keys")
	if s, err := readKeyFile(path); err != nil || s != "" {
		t.Fatalf("missing file: %q %v", s, err)
	}
	if err := writeKeyFile(path, "ssh-ed25519 "+testKey("ssh-ed25519")+" x\n"); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	di, _ := os.Stat(filepath.Dir(path))
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Errorf("modes %v %v", fi.Mode(), di.Mode())
	}
	if _, err := os.Stat(filepath.Join(dir, ".ssh", ".authorized_keys.tmp")); err == nil {
		t.Error("temp file left behind")
	}
	// a symlink must not be followed when reading
	link := filepath.Join(dir, "link")
	os.Symlink(path, link)
	if _, err := readKeyFile(link); err == nil {
		t.Error("symlink followed")
	}
}
