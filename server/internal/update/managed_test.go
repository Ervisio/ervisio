package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/config"
)

func TestManagedBy(t *testing.T) {
	l := testLayout(t)
	if got := l.ManagedBy(); got != "" {
		t.Fatalf("no marker: %q", got)
	}
	l.Managed = filepath.Join(l.LibDir, "managed")
	if got := l.ManagedBy(); got != "" {
		t.Fatalf("marker missing: %q", got)
	}
	for content, want := range map[string]string{
		"pacman\n":              "pacman",
		"  apt  ":               "apt",
		"zypper":                "zypper",
		"":                      "unknown",
		"rm -rf /":              "unknown",
		"Pacman":                "unknown",
		strings.Repeat("a", 40): "unknown",
	} {
		os.WriteFile(l.Managed, []byte(content), 0o644)
		if got := l.ManagedBy(); got != want {
			t.Errorf("marker %q: got %q, want %q", content, got, want)
		}
	}
}

// A packaged install (flat layout + marker) never updates itself: the
// package manager owns /usr/bin/linuxadmind and the other files.
func TestManagedInstallRefusesSelfUpdate(t *testing.T) {
	h := newHarness(t, "1.0.0")
	l := h.u.Layout
	os.RemoveAll(l.VersionsDir())
	os.Remove(filepath.Join(l.LibDir, "current"))
	os.MkdirAll(filepath.Dir(l.BinLink), 0o755)
	os.WriteFile(l.BinLink, []byte("daemon 1.0.0"), 0o755)
	os.WriteFile(filepath.Join(l.LibDir, "linuxadmin-bridge"), []byte("bridge 1.0.0"), 0o755)
	l.Managed = filepath.Join(l.LibDir, "managed")
	os.WriteFile(l.Managed, []byte("apt\n"), 0o644)
	h.gh.publish("1.1.0", h.sk, nil)

	ok, why := h.u.Supported(l.BinLink, false)
	if ok || !strings.Contains(why, "apt") {
		t.Fatalf("supported = %v, %q", ok, why)
	}
	if _, err := h.apply("1.1.0"); !errors.Is(err, ErrManaged) || !strings.Contains(err.Error(), "apt") {
		t.Fatalf("apply: %v", err)
	}
	if _, _, err := h.u.Rollback(context.Background(), ""); !errors.Is(err, ErrManaged) {
		t.Fatalf("rollback: %v", err)
	}
	if _, err := os.Stat(l.VersionsDir()); !os.IsNotExist(err) {
		t.Fatal("versions/ created on a packaged install")
	}
	if fi, err := os.Lstat(l.BinLink); err != nil || !fi.Mode().IsRegular() {
		t.Fatal("the packaged binary was replaced")
	}

	// The automatic install stays off even when configured.
	cfg := config.Default()
	cfg.Updates.AutoInstall = true
	cfg.Updates.AutoInstallAt = "03:30"
	now := time.Date(2026, 10, 2, 3, 30, 0, 0, time.Local)
	a := &Auto{Updater: h.u, Config: func() *config.Config { return cfg }, Now: func() time.Time { return now }}
	a.Tick(context.Background())
	if len(h.launched) != 0 || h.u.State.ReadLast() != nil {
		t.Fatalf("automatic install ran: %v %+v", h.launched, h.u.State.ReadLast())
	}
}
