//go:build !windows

package account

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShellAllowed(t *testing.T) {
	dir := t.TempDir()
	shells := filepath.Join(dir, "shells")
	os.WriteFile(shells, []byte("# comment\n/bin/sh\n/usr/bin/bash\n/usr/bin/zsh\n/usr/bin/git-shell\n/usr/bin/rbash\n/usr/sbin/nologin\n"), 0o644)
	old := ShellsFile
	ShellsFile = shells
	defer func() { ShellsFile = old }()
	for sh, want := range map[string]bool{
		"/usr/bin/bash":      true,
		"/usr/bin/zsh":       true,
		"/bin/sh":            true,
		"/usr/bin/fish":      false, // not listed
		"/usr/sbin/nologin":  false, // listed, but nologin
		"/sbin/nologin":      false,
		"/bin/false":         false,
		"/usr/bin/git-shell": false,
		"/usr/bin/rbash":     false,
		"":                   false,
		"bash":               false,
		"# comment":          false,
	} {
		if got := ShellAllowed(sh); got != want {
			t.Errorf("ShellAllowed(%q) = %v, want %v", sh, got, want)
		}
	}
	ShellsFile = filepath.Join(dir, "missing")
	if !ShellAllowed("/bin/sh") || ShellAllowed("/usr/bin/bash") {
		t.Error("defaults without /etc/shells")
	}
}

func TestReadShadow(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "shadow")
	os.WriteFile(p, []byte("root:!*:19000::::::\nalice:$6$abc$def:19500:0:99999:7:::\nbob:!$6$abc$def:19500:0:99999:7::100:\nalicex:$6$zzz:1:::::\n"), 0o600)
	old := ShadowFile
	ShadowFile = p
	defer func() { ShadowFile = old }()

	a, err := ReadShadow("alice")
	if err != nil || a.Locked || a.Expire != -1 || a.LastChange != 19500 || a.Expired(time.Now()) {
		t.Fatalf("alice: %+v %v", a, err)
	}
	b, err := ReadShadow("bob")
	if err != nil || !b.Locked || !b.Expired(time.Now()) {
		t.Fatalf("bob: %+v %v", b, err)
	}
	if a.Fingerprint == b.Fingerprint || len(a.Fingerprint) != 64 {
		t.Fatal("fingerprints")
	}
	if _, err := ReadShadow("carol"); !errors.Is(err, ErrNoShadowEntry) {
		t.Fatalf("carol: %v", err)
	}
	ShadowFile = filepath.Join(dir, "none")
	if _, err := ReadShadow("alice"); err == nil {
		t.Fatal("missing file")
	}
}

func TestCurrentShell(t *testing.T) {
	a, err := Current()
	if err != nil {
		t.Skip(err)
	}
	if a.Shell == "" {
		t.Fatalf("no shell for %s from NSS", a.Name)
	}
	if nssShell("no-such-user-ervisio-test") != "" {
		t.Fatal("unknown user has a shell")
	}
}
