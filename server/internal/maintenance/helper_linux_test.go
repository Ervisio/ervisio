//go:build linux

package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPackageGuardFindsExternalPackageWorkAndIgnoresDaemons(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "123"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "123", "comm")
	for _, name := range []string{"snapd", "sshd", "ervisiod", "dnf", "dpkg", "apt-get", "pacman", "zypper", "flatpak"} {
		if err := os.WriteFile(path, []byte(name+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		busy, err := packageProcessRunning(context.Background(), dir)
		want := name != "snapd" && name != "sshd" && name != "ervisiod"
		if err != nil || busy != want {
			t.Fatalf("%s: busy=%v err=%v", name, busy, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := packageProcessRunning(ctx, dir); err == nil {
		t.Fatal("cancelled package inventory accepted")
	}
}
