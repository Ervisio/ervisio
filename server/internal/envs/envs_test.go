package envs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/md5" //nolint:gosec
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func ed25519Key() (string, string, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	der, _ := x509.MarshalECPrivateKey(k)
	return "", string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})), nil
}

// selfSigned makes a certificate and key (PEM) valid for 127.0.0.1.
func selfSigned(t *testing.T, client bool) (certPEM, keyPEM string, der []byte) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:    []string{"localhost"},
		IsCA:        true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})), der
}

func TestSplitHostPort(t *testing.T) {
	for _, ok := range []string{"10.0.0.5:2376", "docker.lan:2376", "[::1]:22", "a.b-c.example.org:9001"} {
		if _, _, err := SplitHostPort(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "host", "host:0", "host:70000", "ho st:22", "host:abc", "-bad.example:22", "a_b:22"} {
		if _, _, err := SplitHostPort(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestParseServerURL(t *testing.T) {
	if u, err := ParseServerURL("https://srv.lan:9090/", false); err != nil || u != "https://srv.lan:9090" {
		t.Fatalf("got %q %v", u, err)
	}
	if _, err := ParseServerURL("http://srv.lan:9090", false); err == nil {
		t.Error("plain http to a non-loopback host must need the insecure flag")
	}
	if _, err := ParseServerURL("http://srv.lan:9090", true); err != nil {
		t.Error(err)
	}
	if _, err := ParseServerURL("http://127.0.0.1:9417", false); err != nil {
		t.Error("loopback http is fine:", err)
	}
	for _, bad := range []string{"srv.lan", "ftp://x", "https://a/b", "https://u@h", "https://"} {
		if _, err := ParseServerURL(bad, true); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestValidateTCPTLS(t *testing.T) {
	cert, key, _ := selfSigned(t, true)
	good := Input{Name: "ci", Kind: KindTCPTLS, Address: "10.0.0.5:2376", CA: cert, ClientCert: cert, ClientKey: key}
	e, err := good.Validate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Access.Mode != "all" || e.Secret(SecretClientKey) != key {
		t.Fatalf("unexpected env %+v", e)
	}
	bad := good
	bad.ClientKey = ""
	if _, err := bad.Validate(nil); err == nil {
		t.Error("a client certificate without a key must be refused")
	}
	bad = good
	_, otherKey, _ := selfSigned(t, true)
	bad.ClientKey = otherKey
	if _, err := bad.Validate(nil); err == nil {
		t.Error("a key that does not match the certificate must be refused")
	}
	bad = good
	bad.SkipVerify = true
	bad.Fingerprint = "sha256:" + strings.Repeat("a", 64)
	if _, err := bad.Validate(nil); err == nil {
		t.Error("pin and skip-verify together must be refused")
	}
	// plain tcp drops every TLS setting
	plain := Input{Name: "x", Kind: KindTCPTLS, Address: "10.0.0.5:2375", Insecure: true, ClientCert: cert, ClientKey: key}
	e, err = plain.Validate(nil)
	if err != nil || e.ClientCert != "" || e.Secret(SecretClientKey) != "" || !e.Insecure {
		t.Fatalf("insecure should clear TLS fields: %+v %v", e, err)
	}
	if _, err := (&Input{Name: "", Kind: KindTCPTLS, Address: "a:1"}).Validate(nil); err == nil {
		t.Error("empty name")
	}
}

func TestValidateAccessAndKind(t *testing.T) {
	in := Input{Name: "x", Kind: KindTCPTLS, Address: "a.lan:2375", Insecure: true, Access: &Access{Mode: "restricted", Users: []string{"alice"}, Groups: []string{"ops"}}}
	e, err := in.Validate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Allowed("alice", nil) || !e.Allowed("bob", []string{"ops"}) || e.Allowed("bob", []string{"dev"}) {
		t.Error("restricted access list not applied")
	}
	in.Access = &Access{Mode: "nobody"}
	if _, err := in.Validate(nil); err == nil {
		t.Error("unknown access mode")
	}
	in.Access = &Access{Mode: "restricted", Users: []string{"a b"}}
	if _, err := in.Validate(nil); err == nil {
		t.Error("user name with a space")
	}
	// a kind cannot change on update
	e, _ = (&Input{Name: "x", Kind: KindTCPTLS, Address: "a.lan:2375", Insecure: true}).Validate(nil)
	if _, err := (&Input{Name: "x", Kind: KindSSH}).Validate(e); err == nil {
		t.Error("kind change")
	}
}

func TestServesFamily(t *testing.T) {
	for _, k := range []string{KindTCPTLS, KindSSH, KindPortainerAgent, KindErvisio} {
		if !ServesFamily(k, FamilyDocker) {
			t.Errorf("%s should serve docker", k)
		}
	}
	if ServesFamily("x", FamilyDocker) || ServesFamily(KindSSH, "nope") {
		t.Error("unknown kind or family")
	}
}

// ---- secrets are never serialized ----

func TestSecretsNeverSerialized(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(Options{Dir: dir, TunnelDir: filepath.Join(dir, "t")})
	if err != nil {
		t.Fatal(err)
	}
	cert, key, _ := selfSigned(t, true)
	v, err := m.Create(context.Background(), Input{Name: "tls", Kind: KindTCPTLS, Address: "127.0.0.1:1", CA: cert, ClientCert: cert, ClientKey: key}, "root")
	if err != nil {
		t.Fatal(err)
	}
	e := m.Get(v.ID)
	e.SetSecret(SecretAgent, "SUPERSECRETVALUE")
	for name, obj := range map[string]any{"view": v, "list": m.List(), "env": e, "briefs": m.Briefs("a", nil)} {
		b, _ := json.Marshal(obj)
		for _, leak := range []string{"PRIVATE KEY", "SUPERSECRETVALUE", "clientKey\":\"", key[:60]} {
			if strings.Contains(string(b), leak) {
				t.Errorf("%s JSON leaks %q: %s", name, leak, b)
			}
		}
	}
	if !m.List()[0].HasSecrets[SecretClientKey] {
		t.Error("HasSecrets should say the key is set")
	}
	// On disk: 0600, and the key is sealed (not in clear).
	fi, err := os.Stat(filepath.Join(dir, "envs.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("envs.json mode: %v %v", fi, err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "master.key")); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Error("master.key must be 0600")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "envs.json"))
	if strings.Contains(string(raw), "PRIVATE KEY") || strings.Contains(string(raw), key[40:80]) {
		t.Error("the client key is stored in clear")
	}
	// Reopen: secrets come back.
	m2, err := NewManager(Options{Dir: dir, TunnelDir: filepath.Join(dir, "t")})
	if err != nil {
		t.Fatal(err)
	}
	if got := m2.Get(v.ID).Secret(SecretClientKey); got != key {
		t.Error("the client key did not survive a restart")
	}
	// A replaced master key makes the store refuse to open rather than serve garbage.
	if err := os.WriteFile(filepath.Join(dir, "master.key"), make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(Options{Dir: dir, TunnelDir: filepath.Join(dir, "t")}); err == nil {
		t.Error("opening with the wrong master key must fail")
	}
}

func TestPairingStoresOnlyHash(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(Options{Dir: dir, TunnelDir: dir, ServerName: "b"})
	tok, _, err := m.CreatePairToken("admin", "alice")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.Redeem(tok, "server-a", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Redeem(tok, "server-a", "1.2.3.4"); err == nil {
		t.Fatal("a token must work once")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "envs.json"))
	if strings.Contains(string(raw), res.Credential) || strings.Contains(string(raw), tok) {
		t.Fatal("the credential or token is stored in clear")
	}
	if p := m.AuthPair(res.Credential); p == nil || p.User != "alice" || p.Name != "server-a" {
		t.Fatalf("auth: %+v", p)
	}
	if m.AuthPair(res.Credential+"x") != nil || m.AuthPair("") != nil {
		t.Fatal("a wrong credential must fail")
	}
	if ok, _ := m.RevokePairing(res.PairID); !ok || m.AuthPair(res.Credential) != nil {
		t.Fatal("revoked credential still works")
	}
}

func TestTokenExpiryAndRateLimit(t *testing.T) {
	m, _ := NewManager(Options{Dir: t.TempDir(), ServerName: "b"})
	tok, _, _ := m.CreatePairToken("a", "alice")
	m.pairMu.Lock()
	for _, p := range m.tokens {
		p.expires = time.Now().Add(-time.Second)
	}
	m.pairMu.Unlock()
	if _, err := m.Redeem(tok, "a", "9.9.9.9"); err == nil {
		t.Fatal("expired token accepted")
	}
	for i := 0; i < 12; i++ {
		_, _ = m.Redeem("ept_wrong", "a", "8.8.8.8")
	}
	tok2, _, _ := m.CreatePairToken("a", "alice")
	if _, err := m.Redeem(tok2, "a", "8.8.8.8"); err == nil || !strings.Contains(err.Error(), "Too many") {
		t.Fatalf("expected rate limit, got %v", err)
	}
}

// ---- Portainer agent signature ----

func TestAgentSignatureScheme(t *testing.T) {
	// The message hash is fixed by the protocol: md5("Portainer-App").
	h := md5.Sum([]byte("Portainer-App")) //nolint:gosec
	if hex.EncodeToString(h[:]) != "2cc83b00b667d3d15f0786c56be5daf0" {
		t.Fatal("md5(\"Portainer-App\") changed")
	}
	s, err := NewAgentSigner()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"", "s3cr3t"} {
		hdr := http.Header{}
		if err := s.Sign(hdr, secret); err != nil {
			t.Fatal(err)
		}
		pub, sig := hdr.Get(HeaderPublicKey), hdr.Get(HeaderSignature)
		// Shape the agent expects: hex DER PKIX key, raw-base64 of r||s (64 bytes).
		der, err := hex.DecodeString(pub)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := x509.ParsePKIXPublicKey(der); err != nil {
			t.Fatal(err)
		}
		raw, err := base64.RawStdEncoding.DecodeString(sig)
		if err != nil || len(raw) != 64 {
			t.Fatalf("signature shape: %d %v", len(raw), err)
		}
		if ok, err := VerifyAgentSignature(sig, pub, secret); err != nil || !ok {
			t.Fatalf("agent check (secret %q): %v %v", secret, ok, err)
		}
		// Wrong secret, tampered signature and another message must fail.
		if ok, _ := VerifyAgentSignature(sig, pub, secret+"x"); ok {
			t.Error("verified with the wrong secret")
		}
		bad := []byte(sig)
		if bad[5] == 'A' {
			bad[5] = 'B'
		} else {
			bad[5] = 'A'
		}
		if ok, _ := VerifyAgentSignature(string(bad), pub, secret); ok {
			t.Error("verified a tampered signature")
		}
	}
	// Persisted key round-trips.
	p, _ := s.MarshalPEM()
	s2, err := ParseAgentSigner(p)
	if err != nil || s2.PublicKeyHex() != s.PublicKeyHex() {
		t.Fatalf("PEM round trip: %v", err)
	}
}

// A signature made by the agent project's own test fixture style: r and s
// must be zero-padded to 32 bytes even when they are short.
func TestAgentSignaturePadding(t *testing.T) {
	s, _ := NewAgentSigner()
	for i := 0; i < 200; i++ {
		sig, _ := s.Signature("")
		raw, _ := base64.RawStdEncoding.DecodeString(sig)
		if len(raw) != 64 {
			t.Fatalf("signature is %d bytes", len(raw))
		}
	}
}

// ---- tunnels ----

func shortTemp(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "ervt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// fakeDocker is a TLS server answering GET /version, requiring a client certificate.
func fakeDocker(t *testing.T, certPEM, keyPEM string, requireClient bool) (addr string, stop func()) {
	t.Helper()
	kp, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(certPEM))
	cfg := &tls.Config{Certificates: []tls.Certificate{kp}, MinVersion: tls.VersionTLS12}
	if requireClient {
		cfg.ClientAuth, cfg.ClientCAs = tls.RequireAndVerifyClientCert, pool
	}
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			io.WriteString(w, `{"Version":"27.1.1","ApiVersion":"1.46"}`)
			return
		}
		io.WriteString(w, "pong "+r.URL.Path)
	}))
	ts.TLS = cfg
	ts.StartTLS()
	return ts.Listener.Addr().String(), ts.Close
}

func TestTunnelLifecycleTCPTLS(t *testing.T) {
	cert, key, der := selfSigned(t, true)
	addr, stop := fakeDocker(t, cert, key, true)
	defer stop()
	dir := shortTemp(t)
	m, err := NewManager(Options{Dir: filepath.Join(dir, "state"), TunnelDir: filepath.Join(dir, "tun"), ServerName: "a"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// pinned to the server certificate, with a client certificate
	v, err := m.Create(ctx, Input{Name: "d", Kind: KindTCPTLS, Address: addr, Fingerprint: CertFingerprint(der), ClientCert: cert, ClientKey: key}, "root")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status == nil || !v.Status.Reachable || v.Status.EngineVersion != "27.1.1" {
		t.Fatalf("status: %+v", v.Status)
	}
	e := m.Get(v.ID)
	uid, gid := os.Getuid(), os.Getgid()
	path, err := m.Tunnel(e, uid, gid)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 || fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("tunnel socket: %v %v", fi, err)
	}
	if di, _ := os.Stat(filepath.Dir(path)); di.Mode().Perm() != 0o711 {
		t.Errorf("tunnel folder mode %v", di.Mode().Perm())
	}
	// A second request for the same user reuses it.
	if p2, _ := m.Tunnel(e, uid, gid); p2 != path || m.TunnelCount() != 1 {
		t.Fatal("tunnel not reused")
	}
	get := func(p string) string {
		cl := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		}}}
		r, err := cl.Get("http://docker" + p)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return string(b)
	}
	if got := get("/hello"); got != "pong /hello" {
		t.Fatalf("through the tunnel: %q", got)
	}
	if got := get("/version"); !strings.Contains(got, "27.1.1") {
		t.Fatalf("version: %q", got)
	}
	// Updating the environment closes its tunnel and removes the socket.
	if _, err := m.Update(ctx, v.ID, Input{Name: "d2", Kind: KindTCPTLS, Address: addr, Fingerprint: CertFingerprint(der), ClientCert: cert}); err != nil {
		t.Fatal(err)
	}
	if m.TunnelCount() != 0 {
		t.Fatal("tunnel survived an update")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("socket file survived")
	}
	// The secret stayed (empty ClientKey on update keeps it).
	if m.Get(v.ID).Secret(SecretClientKey) != key {
		t.Fatal("update lost the client key")
	}
	// Idle tunnels are collected; busy ones are not.
	path, _ = m.Tunnel(m.Get(v.ID), uid, gid)
	m.mu.Lock()
	for _, tn := range m.tunnels {
		tn.lastUse.Store(time.Now().Add(-time.Hour).UnixNano())
	}
	m.mu.Unlock()
	m.gcTunnels()
	if m.TunnelCount() != 0 {
		t.Fatal("idle tunnel not collected")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("socket file of an idle tunnel survived")
	}
	// Delete.
	m.Tunnel(m.Get(v.ID), uid, gid)
	if err := m.Delete(ctx, v.ID); err != nil || m.TunnelCount() != 0 || len(m.List()) != 0 {
		t.Fatalf("delete: %v", err)
	}
}

func TestPinMismatchAndMissingClientCert(t *testing.T) {
	cert, key, der := selfSigned(t, true)
	addr, stop := fakeDocker(t, cert, key, true)
	defer stop()
	m, _ := NewManager(Options{Dir: t.TempDir(), TunnelDir: t.TempDir()})
	ctx := context.Background()
	wrong := "sha256:" + strings.Repeat("0", 64)
	v, err := m.Create(ctx, Input{Name: "d", Kind: KindTCPTLS, Address: addr, Fingerprint: wrong, ClientCert: cert, ClientKey: key}, "r")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status.Reachable || !strings.Contains(v.Status.Error, "changed") {
		t.Fatalf("a wrong pin must not connect: %+v", v.Status)
	}
	// No client certificate: the server refuses the handshake.
	v2, err := m.Create(ctx, Input{Name: "d2", Kind: KindTCPTLS, Address: addr, Fingerprint: CertFingerprint(der)}, "r")
	if err != nil {
		t.Fatal(err)
	}
	if v2.Status.Reachable {
		t.Fatal("connected without the required client certificate")
	}
	// Probe returns the fingerprint to confirm.
	pr, err := m.Probe(ctx, Input{Kind: KindTCPTLS, Address: addr})
	if err != nil || pr.Fingerprint != CertFingerprint(der) {
		t.Fatalf("probe: %+v %v", pr, err)
	}
}

// fakeAgent accepts a request only when the Portainer headers verify, like the agent.
func fakeAgent(t *testing.T, cert, key, secret string) (string, func()) {
	t.Helper()
	kp, _ := tls.X509KeyPair([]byte(cert), []byte(key))
	var bound string
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pub, sig := r.Header.Get(HeaderPublicKey), r.Header.Get(HeaderSignature)
		ok, err := VerifyAgentSignature(sig, pub, secret)
		if pub == "" || sig == "" || err != nil || !ok || (secret == "" && bound != "" && bound != pub) {
			http.Error(w, "Invalid request signature", http.StatusForbidden)
			return
		}
		if secret == "" {
			bound = pub
		}
		if strings.HasSuffix(r.URL.Path, "/stream") {
			// What the real agent does: one HTTP chunk that is declared
			// large and filled as data comes (a proxied Docker stream). The
			// first 4 KiB (the agent's own write buffer) now, more after 3 s.
			c, brw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer c.Close()
			io.WriteString(brw, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n100000\r\n"+strings.Repeat("x", 4096))
			brw.Flush()
			time.Sleep(3 * time.Second)
			io.WriteString(brw, "two\n")
			brw.Flush()
			return
		}
		io.WriteString(w, `{"Version":"26.0.0","ApiVersion":"1.45"}`)
	}))
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{kp}}
	ts.StartTLS()
	return ts.Listener.Addr().String(), ts.Close
}

// A streamed answer (logs follow, stats, events) must reach the client chunk
// by chunk through the agent tunnel, not when it ends. The real Portainer
// agent does not flush its own proxy (docs/api/environments.md), but whatever
// it flushes must pass through here at once.
func TestAgentTunnelFlushesEachChunk(t *testing.T) {
	cert, key, der := selfSigned(t, false)
	addr, stop := fakeAgent(t, cert, key, "s")
	defer stop()
	m, _ := NewManager(Options{Dir: t.TempDir(), TunnelDir: shortTemp(t)})
	v, err := m.Create(context.Background(), Input{Name: "agent", Kind: KindPortainerAgent, Address: addr, Fingerprint: CertFingerprint(der), AgentSecret: "s"}, "r")
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Tunnel(m.Get(v.ID), os.Getuid(), os.Getgid())
	if err != nil {
		t.Fatal(err)
	}
	cl := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", p)
	}}}
	start := time.Now()
	r, err := cl.Get("http://docker/v1.45/containers/x/logs/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	first := make([]byte, 4096)
	if _, err := io.ReadFull(r.Body, first); err != nil || time.Since(start) > time.Second {
		t.Fatalf("first 4 KiB after %v: %v", time.Since(start), err)
	}
}

func TestPortainerAgentEnv(t *testing.T) {
	cert, key, der := selfSigned(t, false)
	for _, secret := range []string{"", "topsecret"} {
		addr, stop := fakeAgent(t, cert, key, secret)
		m, _ := NewManager(Options{Dir: t.TempDir(), TunnelDir: shortTemp(t)})
		in := Input{Name: "agent", Kind: KindPortainerAgent, Address: addr, Fingerprint: CertFingerprint(der), AgentSecret: secret}
		v, err := m.Create(context.Background(), in, "r")
		if err != nil {
			t.Fatal(err)
		}
		if !v.Status.Reachable || v.Status.EngineVersion != "26.0.0" {
			t.Fatalf("secret %q: %+v", secret, v.Status)
		}
		// Through a tunnel the request is signed too.
		p, err := m.Tunnel(m.Get(v.ID), os.Getuid(), os.Getgid())
		if err != nil {
			t.Fatal(err)
		}
		cl := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", p)
		}}}
		r, err := cl.Get("http://docker/v1.45/version")
		if err != nil || r.StatusCode != 200 {
			t.Fatalf("tunnel to agent: %v %v", r, err)
		}
		r.Body.Close()
		// A secret mismatch is explained.
		if secret != "" {
			in.AgentSecret = "wrong"
			v, _ := m.Update(context.Background(), v.ID, in)
			if v.Status.Reachable || !strings.Contains(v.Status.Error, "AGENT_SECRET") {
				t.Fatalf("wrong secret: %+v", v.Status)
			}
		}
		stop()
	}
}

// ---- ssh ----

func TestSSHKeyAndHostKey(t *testing.T) {
	if _, err := ParseSSHKey("nonsense", ""); err == nil {
		t.Error("garbage key accepted")
	}
	_, priv, _ := ed25519Key()
	if _, err := ParseSSHKey(priv, ""); err != nil {
		t.Fatal(err)
	}
	pub, _ := ssh.NewPublicKey(&mustKey(t).PublicKey)
	line := FormatHostKey(pub)
	k, err := ParseHostKey(line)
	if err != nil || SSHFingerprint(k) != SSHFingerprint(pub) {
		t.Fatalf("host key round trip: %v", err)
	}
	in := Input{Name: "s", Kind: KindSSH, Address: "h.lan:22", User: "docker", SSHKey: priv, HostKey: line}
	e, err := in.Validate(nil)
	if err != nil || e.Fingerprint != SSHFingerprint(pub) || e.SocketPath != DefaultSSHSock {
		t.Fatalf("validate: %+v %v", e, err)
	}
	in.HostKey = ""
	if _, err := in.Validate(nil); err == nil {
		t.Error("an ssh environment without a pinned host key must be refused")
	}
	in.HostKey, in.User = line, "bad user"
	if _, err := in.Validate(nil); err == nil {
		t.Error("bad user")
	}
	in.User, in.SocketPath = "docker", "/var/run/../etc/x"
	if _, err := in.Validate(nil); err == nil {
		t.Error("socket path with ..")
	}
}

func mustKey(t *testing.T) *ecdsa.PrivateKey {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
