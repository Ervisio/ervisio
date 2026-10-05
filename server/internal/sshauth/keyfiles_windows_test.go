//go:build windows

package sshauth

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/winsec"
)

// Windows checks the owner SID and the DACL instead of POSIX modes
// (TestReadAuthorizedKeysPermissions in sshauth_unix_test.go).
func TestReadAuthorizedKeysOwnerAndSID(t *testing.T) {
	keys := newKeys(t)
	line := authLine(keys[0].signer) + "\n"
	home := filepath.Join(t.TempDir(), "home")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "authorized_keys"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	old := SSHDConfig
	t.Cleanup(func() { SSHDConfig = old })
	SSHDConfig = filepath.Join(t.TempDir(), "none") // default files
	me, err := winsec.ProcessUser()
	if err != nil {
		t.Fatal(err)
	}
	ip := net.ParseIP("127.0.0.1")

	// The file belongs to this process's user, who is the account.
	u := User{Name: "me", Home: home, SID: me.String()}
	if _, err := Authorize(u, keys[0].signer.PublicKey(), ip, time.Now()); err != nil {
		t.Fatal("safe file refused:", err)
	}
	// An account without a SID is refused clearly, never trusted by default.
	u.SID = ""
	if _, err := Authorize(u, keys[0].signer.PublicKey(), ip, time.Now()); err == nil || !strings.Contains(err.Error(), "no SID") {
		t.Fatalf("account without a SID: %v", err)
	}
}
