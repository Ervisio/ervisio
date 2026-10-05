//go:build !windows

package sshauth

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// StrictModes: files or directories writable by group/others are skipped.
func TestReadAuthorizedKeysPermissions(t *testing.T) {
	keys := newKeys(t)
	line := authLine(keys[0].signer) + "\n"
	home := filepath.Join(t.TempDir(), "home")
	sshDir := filepath.Join(home, ".ssh")
	os.MkdirAll(sshDir, 0o700)
	os.Chmod(home, 0o755)
	ak := filepath.Join(sshDir, "authorized_keys")
	os.WriteFile(ak, []byte(line), 0o600)

	dir := t.TempDir()
	old := SSHDConfig
	t.Cleanup(func() { SSHDConfig = old })
	SSHDConfig = filepath.Join(dir, "none") // default files
	u := User{Name: "me", UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), Home: home}
	ip := net.ParseIP("127.0.0.1")

	if _, err := Authorize(u, keys[0].signer.PublicKey(), ip, time.Now()); err != nil {
		t.Fatal("safe file refused:", err)
	}
	os.Chmod(ak, 0o620)
	if _, err := Authorize(u, keys[0].signer.PublicKey(), ip, time.Now()); err == nil || !strings.Contains(err.Error(), "modes") {
		t.Fatal("group-writable file accepted:", err)
	}
	os.Chmod(ak, 0o600)
	os.Chmod(sshDir, 0o777)
	if _, err := Authorize(u, keys[0].signer.PublicKey(), ip, time.Now()); err == nil {
		t.Fatal("world-writable .ssh accepted")
	}
	os.Chmod(sshDir, 0o700)
	os.Chmod(home, 0o775)
	if _, err := Authorize(u, keys[0].signer.PublicKey(), ip, time.Now()); err == nil {
		t.Fatal("group-writable home accepted")
	}
	os.Chmod(home, 0o755)
	// Owner must be the user or root.
	u2 := u
	u2.UID = u.UID + 4242
	if os.Getuid() != 0 {
		if _, err := Authorize(u2, keys[0].signer.PublicKey(), ip, time.Now()); err == nil {
			t.Fatal("file owned by another user accepted")
		}
	}
	// A symlinked authorized_keys is followed (as sshd does) and checked
	// at its target.
	target := filepath.Join(home, "real_keys")
	os.WriteFile(target, []byte(line), 0o600)
	os.Remove(ak)
	os.Symlink(target, ak)
	if _, err := Authorize(u, keys[0].signer.PublicKey(), ip, time.Now()); err != nil {
		t.Fatal("symlinked file refused:", err)
	}
	// authorized_keys2 is read too.
	os.Remove(ak)
	os.WriteFile(filepath.Join(sshDir, "authorized_keys2"), []byte(line), 0o600)
	if _, err := Authorize(u, keys[0].signer.PublicKey(), ip, time.Now()); err != nil {
		t.Fatal("authorized_keys2 not read:", err)
	}
	// A FIFO must not block the reader.
	os.Remove(filepath.Join(sshDir, "authorized_keys2"))
	if err := mkfifo(ak); err == nil {
		done := make(chan error, 1)
		go func() { _, err := Authorize(u, keys[0].signer.PublicKey(), ip, time.Now()); done <- err }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("fifo accepted")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("reader blocked on a FIFO")
		}
	}
}

// from= follows sshd: only * and ? are wildcards, and a malformed CIDR
// block refuses the whole list (a negation must not silently vanish).
