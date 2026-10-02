package sshauth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// ---- challenges ----

func testStore() (*Store, *time.Time) {
	s := NewStore()
	now := time.Unix(1_800_000_000, 0)
	s.Now = func() time.Time { return now }
	return s, &now
}

func TestChallengeSingleUse(t *testing.T) {
	s, _ := testStore()
	c, err := s.Issue("alice", "10.0.0.1", "10.0.0.1", "host:9090")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Nonce) != 43 {
		t.Fatalf("nonce %q", c.Nonce)
	}
	got, err := s.Take(c.Nonce, "alice", "10.0.0.1")
	if err != nil || got.Host != "host:9090" {
		t.Fatal(got, err)
	}
	if _, err := s.Take(c.Nonce, "alice", "10.0.0.1"); !errors.Is(err, ErrChallenge) {
		t.Fatal("nonce reused", err)
	}
}

func TestChallengeExpiry(t *testing.T) {
	s, now := testStore()
	c, _ := s.Issue("alice", "10.0.0.1", "10.0.0.1", "h")
	*now = now.Add(ChallengeTTL)
	if _, err := s.Take(c.Nonce, "alice", "10.0.0.1"); !errors.Is(err, ErrChallenge) {
		t.Fatal("expired nonce accepted")
	}
	c2, _ := s.Issue("alice", "10.0.0.1", "10.0.0.1", "h")
	*now = now.Add(ChallengeTTL + time.Second)
	s.Prune()
	if s.Len() != 0 {
		t.Fatal("prune kept", s.Len())
	}
	if _, err := s.Take(c2.Nonce, "alice", "10.0.0.1"); err == nil {
		t.Fatal("pruned nonce accepted")
	}
}

func TestChallengeBinding(t *testing.T) {
	s, _ := testStore()
	c, _ := s.Issue("alice", "10.0.0.1", "10.0.0.1", "h")
	// Another address: refused, and the nonce is burnt.
	if _, err := s.Take(c.Nonce, "alice", "10.0.0.2"); err == nil {
		t.Fatal("other IP accepted")
	}
	if _, err := s.Take(c.Nonce, "alice", "10.0.0.1"); err == nil {
		t.Fatal("nonce not burnt after a mismatched try")
	}
	c, _ = s.Issue("alice", "10.0.0.1", "10.0.0.1", "h")
	if _, err := s.Take(c.Nonce, "bob", "10.0.0.1"); err == nil {
		t.Fatal("other user accepted")
	}
	if _, err := s.Take("short", "alice", "10.0.0.1"); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestChallengeBounds(t *testing.T) {
	s, _ := testStore()
	s.PerKey, s.Max = 3, 5
	var first *Challenge
	for i := 0; i < 4; i++ {
		c, err := s.Issue("alice", "10.0.0.1", "10.0.0.1", "h")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = c
		}
	}
	if s.Len() != 3 {
		t.Fatalf("per-key bound: %d", s.Len())
	}
	if _, err := s.Take(first.Nonce, "alice", "10.0.0.1"); err == nil {
		t.Fatal("oldest challenge of the key should have been dropped")
	}
	s.Issue("bob", "10.0.0.2", "10.0.0.2", "h")
	s.Issue("bob", "10.0.0.2", "10.0.0.2", "h")
	s.Issue("bob", "10.0.0.2", "10.0.0.2", "h")
	if _, err := s.Issue("carol", "10.0.0.3", "10.0.0.3", "h"); !errors.Is(err, ErrFull) {
		t.Fatalf("store over Max: %v (len %d)", err, s.Len())
	}
}

// ---- signatures ----

type testKey struct {
	name   string
	signer ssh.Signer
	algo   string
}

func newKeys(t *testing.T) []testKey {
	t.Helper()
	var out []testKey
	_, edk, _ := ed25519.GenerateKey(rand.Reader)
	add := func(name string, k crypto.Signer, algo string) {
		s, err := ssh.NewSignerFromKey(k)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, testKey{name, s, algo})
	}
	add("ed25519", edk, ssh.KeyAlgoED25519)
	for _, c := range []struct {
		curve elliptic.Curve
		algo  string
	}{{elliptic.P256(), ssh.KeyAlgoECDSA256}, {elliptic.P384(), ssh.KeyAlgoECDSA384}, {elliptic.P521(), ssh.KeyAlgoECDSA521}} {
		k, _ := ecdsa.GenerateKey(c.curve, rand.Reader)
		add(c.algo, k, c.algo)
	}
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	add("rsa-sha2-256", rk, ssh.KeyAlgoRSASHA256)
	add("rsa-sha2-512", rk, ssh.KeyAlgoRSASHA512)
	return out
}

func sign(t *testing.T, k testKey, msg []byte) string {
	t.Helper()
	as := k.signer.(ssh.AlgorithmSigner)
	sig, err := as.SignWithAlgorithm(rand.Reader, msg, k.algo)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(ssh.Marshal(sig))
}

func authLine(k ssh.Signer) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k.PublicKey())))
}

func TestVerifyEachKeyType(t *testing.T) {
	msg := Message("host:9090", "alice", "nonce")
	for _, k := range newKeys(t) {
		t.Run(k.name, func(t *testing.T) {
			pub, err := ParsePublicKey(authLine(k.signer) + " alice@laptop")
			if err != nil {
				t.Fatal(err)
			}
			sig, err := ParseSignature(sign(t, k, msg))
			if err != nil {
				t.Fatal(err)
			}
			if err := Verify(pub, sig, msg); err != nil {
				t.Fatal(err)
			}
			// Any change to the signed message fails.
			if err := Verify(pub, sig, Message("other:9090", "alice", "nonce")); !errors.Is(err, ErrBadSignature) {
				t.Fatal("other host accepted", err)
			}
			if err := Verify(pub, sig, Message("host:9090", "bob", "nonce")); err == nil {
				t.Fatal("other user accepted")
			}
		})
	}
}

func TestVerifyRefusesWeakRSA(t *testing.T) {
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	s, _ := ssh.NewSignerFromKey(rk)
	pub, _ := ParsePublicKey(authLine(s))
	msg := []byte("m")
	sig, _ := s.(ssh.AlgorithmSigner).SignWithAlgorithm(rand.Reader, msg, ssh.KeyAlgoRSA) // SHA-1
	if err := Verify(pub, sig, msg); !errors.Is(err, ErrBadSignature) {
		t.Fatal("ssh-rsa SHA-1 signature accepted", err)
	}
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	ss, _ := ssh.NewSignerFromKey(small)
	if _, err := ParsePublicKey(authLine(ss)); !errors.Is(err, ErrUnsupportedKey) {
		t.Fatal("1024-bit RSA accepted", err)
	}
	// An ed25519 signature presented with another format name.
	_, edk, _ := ed25519.GenerateKey(rand.Reader)
	es, _ := ssh.NewSignerFromKey(edk)
	epub, _ := ParsePublicKey(authLine(es))
	esig, _ := es.Sign(rand.Reader, msg)
	esig.Format = ssh.KeyAlgoECDSA256
	if err := Verify(epub, esig, msg); err == nil {
		t.Fatal("mismatched format accepted")
	}
}

func TestParsePublicKeyRejects(t *testing.T) {
	for _, l := range []string{"", "ssh-dss AAAAB3NzaC1kc3M=", "ssh-ed25519", "ssh-ed25519 !!!", `from="1.2.3.4" ssh-ed25519 AAAA`,
		"sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QG9wZW5zc2guY29t"} {
		if _, err := ParsePublicKey(l); err == nil {
			t.Errorf("accepted %q", l)
		}
	}
	// Type name and blob type must agree.
	_, edk, _ := ed25519.GenerateKey(rand.Reader)
	es, _ := ssh.NewSignerFromKey(edk)
	f := strings.Fields(authLine(es))
	if _, err := ParsePublicKey("ecdsa-sha2-nistp256 " + f[1]); err == nil {
		t.Error("type mismatch accepted")
	}
	if _, err := ParseSignature("AAAA"); err == nil {
		t.Error("truncated signature accepted")
	}
}

// ---- authorized_keys ----

func TestFindKeyOptions(t *testing.T) {
	keys := newKeys(t)
	k := keys[0].signer
	other := keys[1].signer
	line := authLine(k)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	ip := net.ParseIP("192.168.1.20")
	cases := []struct {
		name string
		data string
		ok   bool
	}{
		{"plain", line + " me@host", true},
		{"other key only", authLine(other.(ssh.Signer)), false},
		{"comment and blank", "# keys\n\n" + line, true},
		{"restrict ok", "restrict,no-pty " + line, true},
		{"from match", `from="192.168.1.*" ` + line, true},
		{"from cidr", `from="10.0.0.0/8,192.168.0.0/16" ` + line, true},
		{"from mismatch", `from="10.0.0.0/8" ` + line, false},
		{"from negated", `from="!192.168.1.20,192.168.1.*" ` + line, false},
		{"from hostname", `from="*.example.com" ` + line, false},
		{"command refused", `command="/usr/bin/backup" ` + line, false},
		{"cert-authority skipped", "cert-authority " + line, false},
		{"expired", `expiry-time="20260101" ` + line, false},
		{"not yet expired", `expiry-time="20270101Z" ` + line, true},
		{"bad expiry", `expiry-time="2027" ` + line, false},
		{"unknown option", `frobnicate ` + line, false},
		{"environment ignored", `environment="A=b" ` + line, true},
		{"first refused, second ok", `command="x" ` + line + "\n" + line, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := FindKey([]byte(c.data), k.PublicKey(), ip, now)
			if (err == nil) != c.ok {
				t.Fatalf("got %v, want ok=%v", err, c.ok)
			}
			if err != nil && !errors.Is(err, ErrNotAuthorized) {
				t.Fatalf("error does not wrap ErrNotAuthorized: %v", err)
			}
		})
	}
}

func TestMatchFromIPv6(t *testing.T) {
	ip := net.ParseIP("2001:db8::5")
	if !MatchFrom("2001:db8::/32", ip) || !MatchFrom("2001:db8:0::5", ip) || MatchFrom("2001:db9::/32", ip) {
		t.Fatal("ipv6 from=")
	}
	if MatchFrom("*", nil) {
		t.Fatal("nil ip matched")
	}
}

func TestAuthorizedKeysFilesConfig(t *testing.T) {
	dir := t.TempDir()
	old := SSHDConfig
	t.Cleanup(func() { SSHDConfig = old })
	SSHDConfig = filepath.Join(dir, "sshd_config")
	u := User{Name: "alice", UID: 1000, Home: "/home/alice"}

	// No file: sshd's default.
	got := AuthorizedKeysFiles(u)
	if strings.Join(got, " ") != "/home/alice/.ssh/authorized_keys /home/alice/.ssh/authorized_keys2" {
		t.Fatal(got)
	}
	os.MkdirAll(filepath.Join(dir, "sshd_config.d"), 0o755)
	os.WriteFile(filepath.Join(dir, "sshd_config.d", "10-keys.conf"), []byte("AuthorizedKeysFile /etc/ssh/keys/%u \"%h/.ssh/my keys\" .ssh/k_%U %%x\n"), 0o644)
	os.WriteFile(SSHDConfig, []byte("# test\nInclude sshd_config.d/*.conf\nAuthorizedKeysFile .ssh/ignored\nMatch User bob\n  AuthorizedKeysFile none\n"), 0o644)
	got = AuthorizedKeysFiles(u)
	want := "/etc/ssh/keys/alice|/home/alice/.ssh/my keys|/home/alice/.ssh/k_1000|/home/alice/%x"
	if strings.Join(got, "|") != want {
		t.Fatalf("got %q", strings.Join(got, "|"))
	}
	// Match blocks are not applied; "none" outside Match disables keys.
	os.WriteFile(SSHDConfig, []byte("Match User alice\nAuthorizedKeysFile /x\n"), 0o644)
	if got := AuthorizedKeysFiles(u); len(got) != 2 {
		t.Fatal("Match block applied", got)
	}
	os.WriteFile(SSHDConfig, []byte("AuthorizedKeysFile=none\n"), 0o644)
	if got := AuthorizedKeysFiles(u); len(got) != 0 {
		t.Fatal("none", got)
	}
}

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
