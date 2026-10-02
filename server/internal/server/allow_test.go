package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/config"
	"golang.org/x/crypto/ssh"
)

func testHolder(mut func(*config.Config)) *configHolder {
	cfg := config.Default()
	if mut != nil {
		mut(cfg)
	}
	return &configHolder{path: "/nonexistent", cfg: cfg, checked: time.Now().Add(time.Hour)}
}

func TestSignInAllowed(t *testing.T) {
	alice := &account.Account{Name: "alice", UID: 1000, GroupNames: []string{"alice", "devs"}}
	bob := &account.Account{Name: "bob", UID: 1001, GroupNames: []string{"bob", "sudo"}}
	root := &account.Account{Name: "root", UID: 0, GroupNames: []string{"root"}}
	cfgOf := func(mut func(*config.Auth)) *config.Config {
		c := config.Default()
		if mut != nil {
			mut(&c.Auth)
		}
		return c
	}
	cases := []struct {
		name string
		mut  func(*config.Auth)
		want [3]bool // alice, bob, root
	}{
		{"unrestricted", nil, [3]bool{true, true, true}},
		{"users", func(a *config.Auth) { a.AllowUsers = []string{"alice"} }, [3]bool{true, false, false}},
		{"supplementary group", func(a *config.Auth) { a.AllowGroups = []string{"devs"} }, [3]bool{true, false, false}},
		{"primary group", func(a *config.Auth) { a.AllowGroups = []string{"bob"} }, [3]bool{false, true, false}},
		{"admins only", func(a *config.Auth) { a.AdminsOnly = true }, [3]bool{false, true, true}},
		{"admins or user", func(a *config.Auth) { a.AdminsOnly = true; a.AllowUsers = []string{"alice"} }, [3]bool{true, true, true}},
		{"unrelated lists", func(a *config.Auth) { a.AllowUsers = []string{"carol"}; a.AllowGroups = []string{"ops"} }, [3]bool{false, false, false}},
	}
	for _, c := range cases {
		cfg := cfgOf(c.mut)
		for i, a := range []*account.Account{alice, bob, root} {
			if got := signInAllowed(cfg, a); got != c.want[i] {
				t.Errorf("%s: %s allowed = %v, want %v", c.name, a.Name, got, c.want[i])
			}
		}
	}
}

func TestRevalidateAllowlist(t *testing.T) {
	a, err := account.Current()
	if err != nil {
		t.Skip(err)
	}
	if !account.ShellAllowed(a.Shell) {
		t.Skipf("current user's shell %q is not a login shell", a.Shell)
	}
	f := &fakeAccounts{acc: a, shadow: &account.ShadowEntry{Fingerprint: "fp1", Expire: -1, LastChange: 19000}}
	s := &Server{log: log.New(io.Discard, "", 0), checker: f.checker(), sessions: newStore(), cfg: testHolder(nil)}
	sess := &Session{Account: a, Created: time.Now(), lastSeen: time.Now(), shadowFP: "fp1"}
	if r := s.revalidate(sess, false); r != "" {
		t.Fatalf("unrestricted: %s", r)
	}
	s.cfg = testHolder(func(c *config.Config) { c.Auth.AllowUsers = []string{a.Name} })
	if r := s.revalidate(sess, false); r != "" {
		t.Fatalf("listed user ended: %s", r)
	}
	// Removing the user from the list ends the session at the next check.
	s.cfg = testHolder(func(c *config.Config) { c.Auth.AllowUsers = []string{"somebody-else"} })
	if r := s.revalidate(sess, false); !strings.Contains(r, "no longer allowed") {
		t.Fatalf("removed from list: %q", r)
	}
	// Likewise when a group they relied on is dropped from allow_groups.
	if len(a.GroupNames) == 0 {
		t.Skip("current user has no named groups")
	}
	s.cfg = testHolder(func(c *config.Config) { c.Auth.AllowGroups = []string{a.GroupNames[0]} })
	if r := s.revalidate(sess, false); r != "" {
		t.Fatalf("group member ended: %s", r)
	}
	s.cfg = testHolder(func(c *config.Config) { c.Auth.AllowGroups = []string{"no-such-group"} })
	if r := s.revalidate(sess, false); r == "" {
		t.Fatal("not in any allowed group but session kept")
	}
}

// SSH-key sign-in honours the allowlist, with an explicit answer once the
// key signature is proven.
func TestKeyLoginAllowlist(t *testing.T) {
	_, edk, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(edk)
	keys := filepath.Join(t.TempDir(), "authorized_keys")
	os.WriteFile(keys, ssh.MarshalAuthorizedKey(signer.PublicKey()), 0o600)
	ts, srv := newKeyTestServer(t, keys)
	me := srv.devUser.Name
	cl := &http.Client{}
	jar, _ := cookiejar.New(nil)
	cl.Jar = jar

	set := func(mut func(*config.Config)) {
		cfg := srv.Config().Clone()
		mut(cfg)
		srv.cfg.mu.Lock()
		srv.cfg.cfg = cfg
		srv.cfg.mu.Unlock()
	}
	set(func(c *config.Config) { c.Auth.AllowUsers = []string{"somebody-else"} })
	ch := getChallenge(t, cl, ts, me)
	if code, body := keyLogin(t, cl, ts, me, signer, ch, []byte(ch.Challenge)); code != 403 || !strings.Contains(body, "not_allowed") || !strings.Contains(body, "may not sign in") {
		t.Fatal(code, body)
	}
	set(func(c *config.Config) { c.Auth.AllowUsers = []string{me} })
	ch = getChallenge(t, cl, ts, me)
	if code, body := keyLogin(t, cl, ts, me, signer, ch, []byte(ch.Challenge)); code != 200 {
		t.Fatal(code, body)
	}
}

func TestSecureCookies(t *testing.T) {
	req := func(remote, proto string) *http.Request {
		h := map[string]string{}
		if proto != "" {
			h["X-Forwarded-Proto"] = proto
		}
		return originReq("127.0.0.1:9090", remote, h)
	}
	tlsSrv := originServer(false, nil)
	if !tlsSrv.secureCookies(req("203.0.113.5:1", "")) {
		t.Fatal("TLS mode: Secure always")
	}
	plain := originServer(false, func(c *config.Config) { c.TLS.Mode = config.TLSHTTP; c.Listen = "127.0.0.1:9090" })
	if !plain.secureCookies(req("127.0.0.1:5000", "https")) || !plain.secureCookies(req("127.0.0.1:5000", "HTTPS, http")) {
		t.Fatal("https via a trusted proxy must be Secure")
	}
	if plain.secureCookies(req("127.0.0.1:5000", "http")) || plain.secureCookies(req("127.0.0.1:5000", "")) {
		t.Fatal("plain http must not be Secure")
	}
	// Spoofed header from a peer that is not a trusted proxy.
	if plain.secureCookies(req("203.0.113.5:1", "https")) {
		t.Fatal("X-Forwarded-Proto from an untrusted peer must be ignored")
	}
	if originServer(true, nil).secureCookies(req("127.0.0.1:5000", "https")) {
		t.Fatal("dev: never Secure")
	}
	// The https origin the browser used is accepted by the plain backend.
	r := originReq("127.0.0.1:9090", "127.0.0.1:5000", map[string]string{"X-Forwarded-Host": "admin.example.com", "X-Forwarded-Proto": "https"})
	if !plain.originAllowed("https://admin.example.com", r) {
		t.Fatal("proxied https origin refused by the http backend")
	}
}

func TestRunPlainHTTPNeedsLoopback(t *testing.T) {
	s := &Server{opts: Options{Listen: "0.0.0.0:0"}, log: log.New(io.Discard, "", 0),
		cfg: testHolder(func(c *config.Config) { c.TLS.Mode = config.TLSHTTP; c.Listen = "127.0.0.1:9090" })}
	err := s.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("--listen override to a public address must be refused: %v", err)
	}
}
