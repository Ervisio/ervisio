package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
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

// Turning allow_root or auth.ssh_keys off ends the sessions they admitted.
func TestRevalidateRootAndKeyPolicy(t *testing.T) {
	a, err := account.Current()
	if err != nil {
		t.Skip(err)
	}
	if !account.ShellAllowed(a.Shell) {
		t.Skipf("current user's shell %q is not a login shell", a.Shell)
	}
	f := &fakeAccounts{acc: a, shadow: &account.ShadowEntry{Fingerprint: "fp1", Expire: -1, LastChange: 19000}}
	s := &Server{log: log.New(io.Discard, "", 0), checker: f.checker(), sessions: newStore(), cfg: testHolder(nil)}
	s.checker.keyAuth = func(*account.Account, ssh.PublicKey, string) error { return nil }

	// A key session ends once auth.ssh_keys is turned off; a password
	// session does not.
	keySess := &Session{Account: a, Created: time.Now(), lastSeen: time.Now(), shadowFP: "fp1", Method: "ssh-key",
		sshKey: &sessionKey{fingerprint: "SHA256:x"}}
	pwSess := &Session{Account: a, Created: time.Now(), lastSeen: time.Now(), shadowFP: "fp1"}
	if r := s.revalidate(keySess, false); r != "" {
		t.Fatalf("key session, keys on: %s", r)
	}
	s.cfg = testHolder(func(c *config.Config) { c.Auth.SSHKeys = false })
	if r := s.revalidate(keySess, false); !strings.Contains(r, "auth.ssh_keys") {
		t.Fatalf("key session kept with auth.ssh_keys = false: %q", r)
	}
	if r := s.revalidate(pwSess, false); r != "" {
		t.Fatalf("password session ended by auth.ssh_keys = false: %s", r)
	}

	// A uid-0 session ends once allow_root is turned off.
	root := *a
	root.UID = 0
	f.acc = &root
	rootSess := &Session{Account: &root, Created: time.Now(), lastSeen: time.Now(), shadowFP: "fp1"}
	s.cfg = testHolder(func(c *config.Config) { c.AllowRoot = true })
	if r := s.revalidate(rootSess, false); r != "" {
		t.Fatalf("root with allow_root: %s", r)
	}
	s.cfg = testHolder(nil)
	if r := s.revalidate(rootSess, false); !strings.Contains(r, "allow_root") {
		t.Fatalf("root session kept with allow_root = false: %q", r)
	}
}

// In tls.mode = "http" the Secure flag fails closed: only an explicit
// X-Forwarded-Proto: http from a trusted proxy, or a loopback host name,
// drops it; a TLS request is always Secure whatever the configured mode.
func TestSecureCookiesFailClosed(t *testing.T) {
	plain := originServer(false, func(c *config.Config) { c.TLS.Mode = config.TLSHTTP; c.Listen = "127.0.0.1:9090" })
	// A TLS-terminating proxy that does not send X-Forwarded-Proto.
	if !plain.secureCookies(originReq("admin.example.com", "127.0.0.1:5000", nil)) {
		t.Fatal("no X-Forwarded-Proto and a public host name must stay Secure")
	}
	// A peer that is not a trusted proxy cannot turn Secure off.
	if !plain.secureCookies(originReq("admin.example.com", "203.0.113.5:1", map[string]string{"X-Forwarded-Proto": "http"})) {
		t.Fatal("X-Forwarded-Proto: http from an untrusted peer must be ignored")
	}
	// The trusted proxy says plain http.
	if plain.secureCookies(originReq("admin.example.com", "127.0.0.1:5000", map[string]string{"X-Forwarded-Proto": "http"})) {
		t.Fatal("trusted proxy over plain http")
	}
	// tls.mode changed to "http" but not restarted yet: still serving TLS.
	r := originReq("admin.example.com", "203.0.113.5:1", nil)
	r.TLS = &tls.ConnectionState{}
	if !plain.secureCookies(r) {
		t.Fatal("a TLS request must always get a Secure cookie")
	}
}

// clientAddr reports when the browser's address is unknown: a trusted
// proxy without (usable) X-Forwarded-For.
func TestClientAddrKnown(t *testing.T) {
	s := originServer(false, nil)
	cases := []struct {
		remote, xff, want string
		known             bool
	}{
		{"203.0.113.5:1", "198.51.100.1", "203.0.113.5", true}, // untrusted peer: header ignored
		{"127.0.0.1:1", "", "127.0.0.1", false},                // proxy that sent nothing
		{"127.0.0.1:1", "bogus", "127.0.0.1", false},           // malformed
		{"127.0.0.1:1", "198.51.100.1", "198.51.100.1", true},  // proxied browser
		{"127.0.0.1:1", "bogus, 198.51.100.1", "198.51.100.1", true},
		{"127.0.0.1:1", "127.0.0.1", "127.0.0.1", true}, // browser on the proxy host
	}
	for _, c := range cases {
		h := map[string]string{}
		if c.xff != "" {
			h["X-Forwarded-For"] = c.xff
		}
		got, known := s.clientAddr(originReq("h", c.remote, h))
		if got != c.want || known != c.known {
			t.Errorf("%s xff=%q: got %s,%v want %s,%v", c.remote, c.xff, got, known, c.want, c.known)
		}
	}
}

// keyLoginHdr is keyLogin with extra request headers.
func keyLoginHdr(t *testing.T, c *http.Client, url, user string, signer ssh.Signer, ch challengeResp, hdr map[string]string) (int, string) {
	t.Helper()
	sig, err := signer.Sign(rand.Reader, []byte(ch.Challenge))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"user":      user,
		"publicKey": strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))),
		"signature": base64.StdEncoding.EncodeToString(ssh.Marshal(sig)),
		"nonce":     ch.Nonce,
	})
	req, _ := http.NewRequest("POST", url+"/api/auth/login-key", strings.NewReader(string(body)))
	req.Header.Set("X-Requested-With", "ervisio")
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// A from= option is never matched against a trusted proxy's own address:
// behind a proxy that sends no X-Forwarded-For, from="127.0.0.1" must not
// let every client in.
func TestKeyLoginFromBehindProxy(t *testing.T) {
	_, edk, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(edk)
	keys := filepath.Join(t.TempDir(), "authorized_keys")
	os.WriteFile(keys, []byte(`from="127.0.0.1" `+string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), 0o600)
	ts, srv := newKeyTestServer(t, keys)
	me := srv.devUser.Name
	jar, _ := cookiejar.New(nil)
	cl := &http.Client{Jar: jar}

	challenge := func(xff string) challengeResp {
		t.Helper()
		req, _ := http.NewRequest("POST", ts.URL+"/api/auth/challenge", strings.NewReader(`{"user":"`+me+`","host":"`+strings.TrimPrefix(ts.URL, "http://")+`"}`))
		req.Header.Set("X-Requested-With", "ervisio")
		req.Header.Set("Content-Type", "application/json")
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		resp, err := cl.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var ch challengeResp
		if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&ch) != nil {
			t.Fatalf("challenge: %d", resp.StatusCode)
		}
		return ch
	}
	xff := func(v string) map[string]string { return map[string]string{"X-Forwarded-For": v} }

	// The test client connects from 127.0.0.1, a trusted proxy by default,
	// without X-Forwarded-For: the browser's address is unknown.
	if code, body := keyLoginHdr(t, cl, ts.URL, me, signer, challenge(""), nil); code != 401 || !strings.Contains(body, "key_refused") {
		t.Fatalf("from= matched against the proxy address: %d %s", code, body)
	}
	// A forwarded address outside from= is refused...
	if code, body := keyLoginHdr(t, cl, ts.URL, me, signer, challenge("198.51.100.7"), xff("198.51.100.7")); code != 401 || !strings.Contains(body, "key_refused") {
		t.Fatalf("from= mismatch accepted: %d %s", code, body)
	}
	// ...one inside it is accepted.
	os.WriteFile(keys, []byte(`from="198.51.100.0/24" `+string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), 0o600)
	if code, body := keyLoginHdr(t, cl, ts.URL, me, signer, challenge("198.51.100.7"), xff("198.51.100.7")); code != 200 {
		t.Fatalf("from= match refused: %d %s", code, body)
	}
	// Revalidation matches from= against the same address.
	sess := srv.sessions.all()[0]
	if sess.sshKey == nil || sess.sshKey.fromIP != "198.51.100.7" {
		t.Fatalf("session key address %+v", sess.sshKey)
	}
	if r := srv.revalidate(sess, false); r != "" {
		t.Fatal("revalidate:", r)
	}
}

// --dev-authorized-keys must be a file only the daemon's user can change,
// and the daemon must listen on loopback.
func TestDevAuthorizedKeysChecks(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	os.WriteFile(good, []byte("\n"), 0o600)
	if _, err := readDevAuthorizedKeys(good); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(dir, "loose")
	os.WriteFile(loose, []byte("\n"), 0o600)
	os.Chmod(loose, 0o666)
	if _, err := readDevAuthorizedKeys(loose); err == nil || !strings.Contains(err.Error(), "writable") {
		t.Fatalf("world-writable file accepted: %v", err)
	}
	link := filepath.Join(dir, "link")
	os.Symlink(good, link)
	if _, err := readDevAuthorizedKeys(link); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := readDevAuthorizedKeys(dir); err == nil {
		t.Fatal("directory accepted")
	}

	// Run refuses a non-loopback listen address and an unsafe file.
	run := func(listen, file string) error {
		s := &Server{opts: Options{Dev: true, Listen: listen, DevAuthorizedKeys: file}, log: log.New(io.Discard, "", 0),
			cfg: testHolder(nil), sessions: newStore(), limiter: newLimiter()}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		s.baseCtx, s.cancel = context.WithCancel(context.Background())
		return s.Run(ctx)
	}
	if err := run("0.0.0.0:0", good); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("public listen accepted: %v", err)
	}
	if err := run("127.0.0.1:0", loose); err == nil || !strings.Contains(err.Error(), "writable") {
		t.Fatalf("unsafe file accepted at start: %v", err)
	}
}
