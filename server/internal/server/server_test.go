package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestLimiter(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		l.fail("1.2.3.4")
	}
	if b, _ := l.blocked("1.2.3.4", 3); !b {
		t.Fatal("should be blocked")
	}
	if b, _ := l.blocked("5.6.7.8", 3); b {
		t.Fatal("other ip blocked")
	}
	now = now.Add(16 * time.Minute)
	if b, _ := l.blocked("1.2.3.4", 3); b {
		t.Fatal("window did not expire")
	}
}

func TestOriginAllowed(t *testing.T) {
	s := &Server{opts: Options{Dev: false}}
	if !s.originAllowed("https://host:9090", "host:9090") || s.originAllowed("https://evil", "host:9090") || s.originAllowed("http://localhost:5173", "host:9090") {
		t.Fatal("prod origin rules")
	}
	s.opts.Dev = true
	if !s.originAllowed("http://localhost:5173", "127.0.0.1:9090") || !s.originAllowed("http://127.0.0.1:5173", "x") || s.originAllowed("http://10.0.0.1", "x") {
		t.Fatal("dev origin rules")
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

func newTestServer(t *testing.T, noAuth bool) *httptest.Server {
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
	return ts
}

func do(t *testing.T, method, url, body string, csrf bool) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if csrf {
		req.Header.Set("X-Requested-With", "linuxadmin")
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestUnauthenticated(t *testing.T) {
	ts := newTestServer(t, false)
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

func TestStatic(t *testing.T) {
	ts := newTestServer(t, false)
	for path, want := range map[string]int{"/": 200, "/services/sshd": 200, "/assets/app.js": 200, "/assets/missing.js": 404} {
		if code, _ := do(t, "GET", ts.URL+path, "", false); code != want {
			t.Errorf("%s: %d", path, code)
		}
	}
}

func TestRPCAndStream(t *testing.T) {
	ts := newTestServer(t, true)
	code, body := do(t, "POST", ts.URL+"/api/rpc", `{"method":"system.host"}`, true)
	if code != 200 || !strings.Contains(body, `"hostname"`) {
		t.Fatal(code, body)
	}
	if code, _ := do(t, "POST", ts.URL+"/api/rpc", `{"method":"system.host"}`, false); code != 403 {
		t.Fatal("csrf", code)
	}
	if code, body := do(t, "POST", ts.URL+"/api/rpc", `{"method":"system.power","params":{"action":"reboot"}}`, true); code != 403 || !strings.Contains(body, "needs_admin") {
		t.Fatal(code, body)
	}
	if code, body := do(t, "POST", ts.URL+"/api/rpc", `{"method":"bad method"}`, true); code != 400 {
		t.Fatal(code, body)
	}
	if code, body := do(t, "GET", ts.URL+"/api/auth/session", "", false); code != 200 || !strings.Contains(body, `"canSudo"`) {
		t.Fatal(code, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/ws", nil)
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
