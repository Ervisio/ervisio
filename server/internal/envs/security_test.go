package envs

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// H1: the per-user tunnel folder belongs to the daemon (0711), not to the
// user, so the user cannot plant a symlink where the daemon creates, chmods
// or chowns the socket. A folder left by an older version (the user's, 0700,
// with whatever the user put in it) is replaced.
func TestTunnelFolderIsTheDaemonsAndReplacesAnOldOne(t *testing.T) {
	cert, key, der := selfSigned(t, true)
	addr, stop := fakeDocker(t, cert, key, true)
	defer stop()
	dir := shortTemp(t)
	tun := filepath.Join(dir, "tun")
	m, err := NewManager(Options{Dir: filepath.Join(dir, "state"), TunnelDir: tun, ServerName: "a"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := m.Create(t.Context(), Input{Name: "d", Kind: KindTCPTLS, Address: addr, Fingerprint: CertFingerprint(der), ClientCert: cert, ClientKey: key}, "root")
	if err != nil {
		t.Fatal(err)
	}
	uid, gid := os.Getuid(), os.Getgid()
	// The old layout: a 0700 folder the user controls, holding a symlink
	// named like the socket that points at a file the user wants.
	old := filepath.Join(tun, strconv.Itoa(uid))
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "victim")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(old, v.ID+".sock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(old, "planted")); err != nil {
		t.Fatal(err)
	}
	path, err := m.Tunnel(m.Get(v.ID), uid, gid)
	if err != nil {
		t.Fatal(err)
	}
	di, err := os.Lstat(filepath.Dir(path))
	if err != nil || !di.IsDir() || di.Mode().Perm() != 0o711 {
		t.Fatalf("tunnel folder: %v %v", di, err)
	}
	if _, err := os.Lstat(filepath.Join(old, "planted")); !os.IsNotExist(err) {
		t.Fatal("the old folder's content was kept")
	}
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket: %v %v", fi, err)
	}
	if ti, _ := os.Stat(target); ti.Mode().Perm() != 0o644 {
		t.Fatalf("the symlink target was changed: %v", ti.Mode())
	}
	// No staging folder is left behind.
	ents, _ := os.ReadDir(tun)
	for _, e := range ents {
		if e.Name() != strconv.Itoa(uid) {
			t.Errorf("left in the tunnel folder: %s", e.Name())
		}
	}
	m.closeTunnels(v.ID)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("socket not removed on close")
	}
	// A symlink in place of the base folder is refused.
	link := filepath.Join(dir, "tunlink")
	os.Symlink(filepath.Join(dir, "state"), link)
	if err := prepareTunnelDir(link, filepath.Join(link, "1")); err == nil {
		t.Fatal("a symlinked base was accepted")
	}
}

func TestDisplayAddressIsNonSecret(t *testing.T) {
	for _, c := range []struct {
		e    Env
		want string
	}{
		{Env{Kind: KindTCPTLS, Address: "10.0.0.5:2376"}, "10.0.0.5:2376"},
		{Env{Kind: KindPortainerAgent, Address: "agent.lan:9001"}, "agent.lan:9001"},
		{Env{Kind: KindSSH, Address: "box:22", User: "deploy"}, "deploy@box:22"},
		{Env{Kind: KindSSH, Address: "box:22"}, "box:22"},
		{Env{Kind: KindErvisio, Address: "https://tok:pw@srv.lan:9090/x?y=1"}, "srv.lan:9090"},
		{Env{Kind: KindErvisio, Address: "::bad"}, ""},
	} {
		if got := c.e.DisplayAddress(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.e, got, c.want)
		}
	}
}
