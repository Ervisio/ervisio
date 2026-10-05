package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ervisio/ervisio/server/internal/sshauth"
	"golang.org/x/crypto/ssh"
)

// newKeyTestServer is a dev server whose SSH-key sign-in reads keysFile.
func newKeyTestServer(t *testing.T, keysFile string) (*httptest.Server, *Server) {
	t.Helper()
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html>app"), 0o644)
	srv, err := New(Options{
		ConfigPath:        filepath.Join(t.TempDir(), "ervisio.conf"),
		Dev:               true,
		WebDir:            web,
		Bridge:            buildBridge(t),
		DevAuthorizedKeys: keysFile,
		Logger:            log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.pamAcctHook = func(string, string) error { return nil }
	old := keyFailMinDelay
	keyFailMinDelay = 0
	t.Cleanup(func() { keyFailMinDelay = old })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		for _, s := range srv.sessions.all() {
			srv.sessions.remove(s)
		}
	})
	return ts, srv
}

// jq returns s as a JSON string literal: a Windows account name holds a
// backslash ("HOST\\user"), which pasted into JSON would be an escape.
func jq(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

type challengeResp struct {
	Nonce     string `json:"nonce"`
	Challenge string `json:"challenge"`
	Host      string `json:"host"`
}

func getChallenge(t *testing.T, c *http.Client, ts *httptest.Server, user string) challengeResp {
	t.Helper()
	code, body := doc(t, c, "POST", ts.URL+"/api/auth/challenge", `{"user":`+jq(user)+`,"host":"`+strings.TrimPrefix(ts.URL, "http://")+`"}`, true)
	if code != 200 {
		t.Fatalf("challenge: %d %s", code, body)
	}
	var r challengeResp
	json.Unmarshal([]byte(body), &r)
	return r
}

func keyLogin(t *testing.T, c *http.Client, ts *httptest.Server, user string, signer ssh.Signer, ch challengeResp, msg []byte) (int, string) {
	t.Helper()
	sig, err := signer.Sign(rand.Reader, msg)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"user":      user,
		"publicKey": strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))),
		"signature": base64.StdEncoding.EncodeToString(ssh.Marshal(sig)),
		"nonce":     ch.Nonce,
		"remember":  false,
	})
	return doc(t, c, "POST", ts.URL+"/api/auth/login-key", string(body), true)
}

func TestKeyLogin(t *testing.T) {
	_, edk, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(edk)
	_, otherk, _ := ed25519.GenerateKey(rand.Reader)
	other, _ := ssh.NewSignerFromKey(otherk)
	keys := filepath.Join(t.TempDir(), "authorized_keys")
	os.WriteFile(keys, ssh.MarshalAuthorizedKey(signer.PublicKey()), 0o600)
	ts, srv := newKeyTestServer(t, keys)
	me := srv.devUser.Name
	jar, _ := cookiejar.New(nil)
	cl := &http.Client{Jar: jar}

	// The challenge is the exact message to sign.
	ch := getChallenge(t, cl, ts, me)
	if ch.Challenge != string(sshauth.Message(ch.Host, me, ch.Nonce)) || !strings.HasPrefix(ch.Challenge, "ervisio-ssh-auth-v1\n127.0.0.1:") {
		t.Fatalf("challenge %q", ch.Challenge)
	}
	// Unknown users get the same kind of answer (no enumeration).
	if code, body := doc(t, cl, "POST", ts.URL+"/api/auth/challenge", `{"user":"nobody-here-xyz"}`, true); code != 200 || !strings.Contains(body, `"nonce"`) {
		t.Fatal(code, body)
	}
	// A host the server does not answer to is refused.
	if code, _ := doc(t, cl, "POST", ts.URL+"/api/auth/challenge", `{"user":`+jq(me)+`,"host":"evil.example"}`, true); code != 400 {
		t.Fatal("foreign host", code)
	}

	// A key that is not listed.
	if code, body := keyLogin(t, cl, ts, me, other, ch, []byte(ch.Challenge)); code != 401 || !strings.Contains(body, "key_refused") {
		t.Fatal(code, body)
	}
	// The nonce was used up by that attempt.
	if code, body := keyLogin(t, cl, ts, me, signer, ch, []byte(ch.Challenge)); code != 401 || !strings.Contains(body, "challenge_invalid") {
		t.Fatal(code, body)
	}
	// A signature over something else (no domain prefix).
	ch = getChallenge(t, cl, ts, me)
	if code, body := keyLogin(t, cl, ts, me, signer, ch, []byte(ch.Nonce)); code != 401 || !strings.Contains(body, "key_refused") {
		t.Fatal(code, body)
	}

	// Success.
	ch = getChallenge(t, cl, ts, me)
	code, body := keyLogin(t, cl, ts, me, signer, ch, []byte(ch.Challenge))
	if code != 200 || !strings.Contains(body, `"authMethod":"ssh-key"`) || !strings.Contains(body, `"keyFingerprint":"SHA256:`) {
		t.Fatal(code, body)
	}
	if code, body := doc(t, cl, "GET", ts.URL+"/api/auth/session", "", false); code != 200 || !strings.Contains(body, `"ssh-key"`) {
		t.Fatal(code, body)
	}

	// Revalidation ends the session once the key leaves authorized_keys.
	sess := srv.sessions.all()[0]
	if r := srv.revalidate(sess, false); r != "" {
		t.Fatal("revalidate:", r)
	}
	os.WriteFile(keys, ssh.MarshalAuthorizedKey(other.PublicKey()), 0o600)
	if r := srv.revalidate(sess, false); !strings.Contains(r, "no longer authorized") {
		t.Fatalf("revalidate after removal: %q", r)
	}
}

func TestKeyLoginDisabledAndRoot(t *testing.T) {
	ts, srv := newKeyTestServer(t, filepath.Join(t.TempDir(), "none"))
	if code, body := do(t, "POST", ts.URL+"/api/auth/challenge", `{"user":"root"}`, true); code != 403 || !strings.Contains(body, "root_disabled") {
		t.Fatal(code, body)
	}
	if code, _ := do(t, "POST", ts.URL+"/api/auth/challenge", `{"user":"x"}`, false); code != 403 {
		t.Fatal("csrf header not required", code)
	}
	cfg := srv.Config().Clone()
	cfg.Auth.SSHKeys = false
	srv.cfg.mu.Lock()
	srv.cfg.cfg = cfg
	srv.cfg.mu.Unlock()
	for _, p := range []string{"/api/auth/challenge", "/api/auth/login-key"} {
		if code, body := do(t, "POST", ts.URL+p, `{"user":"x"}`, true); code != 403 || !strings.Contains(body, "ssh_keys_disabled") {
			t.Fatal(p, code, body)
		}
	}
}

// Failed key sign-ins share the password limiter: after max failures,
// both challenges and key logins are refused with 429.
func TestKeyLoginRateLimited(t *testing.T) {
	ts, srv := newKeyTestServer(t, filepath.Join(t.TempDir(), "none"))
	me := srv.devUser.Name
	_, edk, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(edk)
	jar, _ := cookiejar.New(nil)
	cl := &http.Client{Jar: jar}
	for i := 0; i < 5; i++ {
		ch := getChallenge(t, cl, ts, me)
		if code, body := keyLogin(t, cl, ts, me, signer, ch, []byte(ch.Challenge)); code != 401 {
			t.Fatalf("attempt %d: %d %s", i, code, body)
		}
	}
	if code, body := doc(t, cl, "POST", ts.URL+"/api/auth/challenge", `{"user":`+jq(me)+`}`, true); code != 429 || !strings.Contains(body, "rate_limited") {
		t.Fatal(code, body)
	}
	if code, _ := doc(t, cl, "POST", ts.URL+"/api/auth/login", `{"user":`+jq(me)+`,"password":"x"}`, true); code != 429 {
		t.Fatal("password login not limited after key failures", code)
	}
}
