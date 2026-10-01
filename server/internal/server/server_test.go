package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/brand"
	"github.com/coder/websocket"
)

func TestLimiter(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		a, rej := l.begin("1.2.3.4", 3)
		if rej != nil {
			t.Fatalf("attempt %d refused", i)
		}
		a.done(attemptFailed)
	}
	if b, _ := l.blocked("1.2.3.4", 3); !b {
		t.Fatal("should be blocked")
	}
	if _, rej := l.begin("1.2.3.4", 3); rej == nil || rej.busy || rej.wait <= 0 {
		t.Fatalf("begin should be rate limited: %+v", rej)
	}
	if b, _ := l.blocked("5.6.7.8", 3); b {
		t.Fatal("other ip blocked")
	}
	now = now.Add(16 * time.Minute)
	if b, _ := l.blocked("1.2.3.4", 3); b {
		t.Fatal("window did not expire")
	}
	// Success clears, neutral takes the attempt back only.
	a, _ := l.begin("9.9.9.9", 3)
	a.done(attemptFailed)
	a, _ = l.begin("9.9.9.9", 3)
	a.done(attemptNeutral)
	if n := len(l.failures["9.9.9.9"]); n != 1 {
		t.Fatalf("neutral attempt kept: %d failures", n)
	}
	a, _ = l.begin("9.9.9.9", 3)
	a.done(attemptOK)
	a.done(attemptFailed) // second done is ignored
	if len(l.failures["9.9.9.9"]) != 0 || len(l.inflight) != 0 {
		t.Fatalf("success should clear: %v %v", l.failures, l.inflight)
	}
}

// Concurrent attempts from one client are counted before PAM answers:
// however many run at once, no more than max reach PAM in a window, and
// at most maxInflightPerKey at the same time.
func TestLimiterConcurrent(t *testing.T) {
	l := newLimiter()
	const max = 5
	var mu sync.Mutex
	inflight, peak, admitted := 0, 0, 0
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var a *attempt
			for {
				var rej *rejection
				a, rej = l.begin(limiterKey("2001:db8:1:2::77"), max)
				if rej == nil {
					break
				}
				if !rej.busy {
					return // rate limited
				}
				time.Sleep(time.Millisecond)
			}
			mu.Lock()
			admitted++
			inflight++
			peak = maxInt(peak, inflight)
			mu.Unlock()
			time.Sleep(5 * time.Millisecond) // "PAM"
			mu.Lock()
			inflight--
			mu.Unlock()
			a.done(attemptFailed)
		}()
	}
	close(start)
	wg.Wait()
	if admitted != max {
		t.Fatalf("%d attempts reached PAM, want exactly %d", admitted, max)
	}
	if peak > maxInflightPerKey {
		t.Fatalf("%d attempts ran at once, limit %d", peak, maxInflightPerKey)
	}
	if b, _ := l.blocked(limiterKey("2001:db8:1:2:ffff::1"), max); !b {
		t.Fatal("the whole /64 should be blocked")
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestLimiterKey(t *testing.T) {
	for in, want := range map[string]string{
		"192.0.2.7":              "192.0.2.7",
		"::ffff:192.0.2.7":       "192.0.2.7",
		"2001:db8:aa:bb:1:2:3:4": "2001:db8:aa:bb::/64",
		"2001:db8:aa:bb:ffff::":  "2001:db8:aa:bb::/64",
		"::1":                    "::/64",
		"not-an-ip":              "not-an-ip",
	} {
		if got := limiterKey(in); got != want {
			t.Errorf("limiterKey(%q) = %q, want %q", in, got, want)
		}
	}
	if limiterKey("2001:db8:aa:bb::1") == limiterKey("2001:db8:aa:bc::1") {
		t.Error("different /64 prefixes share a key")
	}
}

func TestOriginAllowed(t *testing.T) {
	s := &Server{opts: Options{Dev: false}}
	if !s.originAllowed("https://host:9090", "host:9090") || s.originAllowed("https://evil", "host:9090") || s.originAllowed("http://localhost:5173", "host:9090") {
		t.Fatal("prod origin rules")
	}
	s.opts.Dev = true
	s.viteHost = "127.0.0.1:5173"
	if !s.originAllowed("http://127.0.0.1:5173", "127.0.0.1:9090") || !s.originAllowed("http://127.0.0.1:9090", "127.0.0.1:9090") {
		t.Fatal("dev: own host and Vite must be accepted")
	}
	// Other local ports share the cookie but are not the app.
	if s.originAllowed("http://localhost:3000", "127.0.0.1:9090") || s.originAllowed("http://127.0.0.1:8080", "127.0.0.1:9090") || s.originAllowed("http://10.0.0.1", "x") {
		t.Fatal("dev: other origins must be refused")
	}
}

func TestLoopbackHost(t *testing.T) {
	for h, want := range map[string]bool{
		"localhost": true, "LOCALHOST:9090": true, "127.0.0.1:9090": true, "127.1.2.3": true,
		"[::1]:9090": true, "[::1]": true, "::1": true,
		"evil.example:9090": false, "localhost.evil.example": false, "10.0.0.1:9090": false,
		"[::ffff:10.0.0.1]:80": false, "": false, "0.0.0.0:9090": false,
	} {
		if got := loopbackHost(h); got != want {
			t.Errorf("loopbackHost(%q) = %v, want %v", h, got, want)
		}
	}
}

func TestSessionLifetime(t *testing.T) {
	if sessionLifetime(false, 168*time.Hour) != 24*time.Hour || sessionLifetime(true, time.Hour) != 24*time.Hour || sessionLifetime(true, 168*time.Hour) != 168*time.Hour {
		t.Fatal("session lifetime")
	}
}

func buildBridge(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "linuxadmin-bridge")
	if b, err := exec.Command("go", "build", "-o", out, "github.com/Fonlogen/LinuxAdmin/server/cmd/linuxadmin-bridge").CombinedOutput(); err != nil {
		t.Fatalf("build bridge: %v\n%s", err, b)
	}
	return out
}

func newTestServer(t *testing.T, noAuth bool) (*httptest.Server, *Server) {
	t.Helper()
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html>app"), 0o644)
	os.MkdirAll(filepath.Join(web, "assets"), 0o755)
	os.WriteFile(filepath.Join(web, "assets", "app.js"), []byte("console.log(1)"), 0o644)
	srv, err := New(Options{
		ConfigPath: filepath.Join(t.TempDir(), "linuxadmin.conf"),
		Dev:        true,
		NoAuth:     noAuth,
		WebDir:     web,
		Bridge:     buildBridge(t),
		Logger:     log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		for _, s := range srv.sessions.all() {
			srv.sessions.remove(s)
		}
	})
	return ts, srv
}

// noAuthClient redeems a --dev-insecure-noauth one-time token and returns
// an HTTP client carrying the session cookie.
func noAuthClient(t *testing.T, srv *Server, ts *httptest.Server) *http.Client {
	t.Helper()
	u, err := srv.newNoAuthToken(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther { // redirected to the app
		t.Fatalf("noauth sign-in: %d", resp.StatusCode)
	}
	// The token works once.
	resp, err = http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("reused token: %d", resp.StatusCode)
	}
	return c
}

func cookieHeader(c *http.Client, rawURL string) http.Header {
	u, _ := url.Parse(rawURL)
	h := http.Header{}
	for _, ck := range c.Jar.Cookies(u) {
		h.Add("Cookie", ck.Name+"="+ck.Value)
	}
	return h
}

func do(t *testing.T, method, url, body string, csrf bool) (int, string) {
	t.Helper()
	return doc(t, http.DefaultClient, method, url, body, csrf)
}

func doc(t *testing.T, client *http.Client, method, url, body string, csrf bool) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if csrf {
		req.Header.Set("X-Requested-With", "linuxadmin")
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestUnauthenticated(t *testing.T) {
	ts, _ := newTestServer(t, false)
	if code, _ := do(t, "GET", ts.URL+"/api/public/host", "", false); code != 200 {
		t.Fatal("public host", code)
	}
	if code, body := do(t, "GET", ts.URL+"/api/auth/session", "", false); code != 401 || !strings.Contains(body, "unauthenticated") {
		t.Fatal(code, body)
	}
	if code, body := do(t, "POST", ts.URL+"/api/rpc", `{"method":"system.host"}`, true); code != 401 || !strings.Contains(body, "unauthenticated") {
		t.Fatal(code, body)
	}
	if code, _ := do(t, "POST", ts.URL+"/api/auth/login", `{"user":"x","password":"y"}`, false); code != 403 {
		t.Fatal("login without csrf header", code)
	}
	if code, body := do(t, "POST", ts.URL+"/api/auth/login", `{"user":"root","password":"y"}`, true); code != 403 || !strings.Contains(body, "root_disabled") {
		t.Fatal(code, body)
	}
}

func TestHealth(t *testing.T) {
	ts, _ := newTestServer(t, false)
	code, body := do(t, "GET", ts.URL+"/api/health", "", false)
	if code != 200 || !strings.Contains(body, `"version":"`+brand.Version+`"`) || !strings.Contains(body, `"startedAt":`) {
		t.Fatal(code, body)
	}
}

func TestStatic(t *testing.T) {
	ts, _ := newTestServer(t, false)
	for path, want := range map[string]int{"/": 200, "/services/sshd": 200, "/assets/app.js": 200, "/assets/missing.js": 404} {
		if code, _ := do(t, "GET", ts.URL+path, "", false); code != want {
			t.Errorf("%s: %d", path, code)
		}
	}
}

func TestRPCAndStream(t *testing.T) {
	ts, srv := newTestServer(t, true)
	// Without the one-time token, noauth signs nobody in.
	if code, _ := do(t, "POST", ts.URL+"/api/rpc", `{"method":"system.host"}`, true); code != 401 {
		t.Fatal("noauth without token", code)
	}
	cl := noAuthClient(t, srv, ts)
	code, body := doc(t, cl, "POST", ts.URL+"/api/rpc", `{"method":"system.host"}`, true)
	if code != 200 || !strings.Contains(body, `"hostname"`) {
		t.Fatal(code, body)
	}
	if code, _ := doc(t, cl, "POST", ts.URL+"/api/rpc", `{"method":"system.host"}`, false); code != 403 {
		t.Fatal("csrf", code)
	}
	if code, body := doc(t, cl, "POST", ts.URL+"/api/rpc", `{"method":"system.power","params":{"action":"reboot"}}`, true); code != 403 || !strings.Contains(body, "needs_admin") {
		t.Fatal(code, body)
	}
	if code, body := doc(t, cl, "POST", ts.URL+"/api/rpc", `{"method":"bad method"}`, true); code != 400 {
		t.Fatal(code, body)
	}
	if code, body := doc(t, cl, "GET", ts.URL+"/api/auth/session", "", false); code != 200 || !strings.Contains(body, `"canSudo"`) {
		t.Fatal(code, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/ws", &websocket.DialOptions{HTTPHeader: cookieHeader(cl, ts.URL)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.Write(ctx, websocket.MessageText, []byte(`{"ch":3,"op":"open","method":"system.metricsStream","params":{"interval":250}}`))
	for i := 0; i < 2; i++ {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var f wsFrame
		json.Unmarshal(data, &f)
		if f.Ch != 3 || f.Op != "data" || !strings.Contains(string(f.Data), `"cpu"`) {
			t.Fatalf("frame %s", data)
		}
	}
	c.Write(ctx, websocket.MessageText, []byte(`{"ch":3,"op":"close"}`))
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var f wsFrame
		json.Unmarshal(data, &f)
		if f.Op == "end" {
			break
		}
		if f.Op != "data" {
			t.Fatalf("frame %s", data)
		}
	}
	c.Write(ctx, websocket.MessageText, []byte(`{"ch":4,"op":"open","method":"system.power"}`))
	_, data, _ := c.Read(ctx)
	if !strings.Contains(string(data), `"needs_admin"`) {
		t.Fatalf("frame %s", data)
	}
}

// Failed sign-ins are counted per client and end in 429 rate_limited.
func TestLoginRateLimited(t *testing.T) {
	ts, _ := newTestServer(t, false)
	for i := 0; i < 5; i++ { // default login.max_failures
		code, body := do(t, "POST", ts.URL+"/api/auth/login", `{"user":"not-the-dev-user","password":"x"}`, true)
		if code != 403 || !strings.Contains(body, "dev_mode_user") {
			t.Fatalf("attempt %d: %d %s", i, code, body)
		}
	}
	code, body := do(t, "POST", ts.URL+"/api/auth/login", `{"user":"not-the-dev-user","password":"x"}`, true)
	if code != 429 || !strings.Contains(body, "rate_limited") {
		t.Fatalf("after max failures: %d %s", code, body)
	}
}
