package server

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newJobsServer is a --dev-insecure-noauth server whose jobs and channels live in temp folders.
func newJobsServer(t *testing.T, noAuth bool) (*httptest.Server, *Server, string) {
	t.Helper()
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html>app"), 0o644)
	state := t.TempDir()
	srv, err := New(Options{
		ConfigPath: filepath.Join(t.TempDir(), "ervisio.conf"),
		Dev:        true,
		NoAuth:     noAuth,
		WebDir:     web,
		Bridge:     buildBridge(t),
		StateDir:   state,
		NotifyFile: filepath.Join(state, "notify.json"),
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
	return ts, srv, state
}

func TestWebhookEndpointNeedsNoSessionAndSaysLittle(t *testing.T) {
	ts, _, _ := newJobsServer(t, false)
	// No cookie, no CSRF header: an unknown token is a plain 404 with a generic body.
	req, _ := http.NewRequest("POST", ts.URL+"/hooks/docker/not-a-real-token", strings.NewReader(`{"a":1}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 404 || strings.TrimSpace(string(b)) != `{"error":"not found"}` {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Error("answers must not be cached")
	}
	// A client that keeps guessing is slowed down.
	last := 0
	for i := 0; i < 40; i++ {
		resp, err := http.Post(ts.URL+"/hooks/docker/guess"+string(rune('a'+i%26)), "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		last = resp.StatusCode
	}
	if last != 429 {
		t.Fatalf("guessing was not slowed down: %d", last)
	}
}

func TestDaemonMethodsNeedASession(t *testing.T) {
	ts, _, _ := newJobsServer(t, false)
	for _, m := range []string{"plugins.jobs.list", "plugins.notify", "notify.list", "jobs.list"} {
		if code, body := do(t, "POST", ts.URL+"/api/rpc", `{"method":"`+m+`","params":{"plugin":"x"}}`, true); code != 401 {
			t.Errorf("%s: %d %s", m, code, body)
		}
	}
}

func TestChannelsOverRPCNeverReturnSecrets(t *testing.T) {
	ts, srv, state := newJobsServer(t, true)
	c := noAuthClient(t, srv, ts)
	rpc := func(method, params string) (int, string) {
		return doc(t, c, "POST", ts.URL+"/api/rpc", `{"method":"`+method+`","params":`+params+`}`, true)
	}
	if code, body := rpc("plugins.jobs.list", `{"plugin":"docker"}`); code != 200 || !strings.Contains(body, `"instances":[]`) {
		t.Fatalf("empty list: %d %s", code, body)
	}
	if code, body := rpc("plugins.jobs.list", `{}`); code != 400 {
		t.Fatalf("a plugin-scoped call without a plugin: %d %s", code, body)
	}
	code, body := rpc("notify.save", `{"name":"n","type":"telegram","botToken":"nope","chatId":"1"}`)
	if code != 400 || !strings.Contains(body, "bot token") {
		t.Fatalf("invalid channel: %d %s", code, body)
	}
	code, body = rpc("notify.save", `{"name":"hook","type":"webhook","enabled":true,"url":"https://hooks.example.org/services/SECRETPART","headerName":"X-T","headerValue":"hdrsecret"}`)
	if code != 200 || strings.Contains(body, "SECRETPART") || strings.Contains(body, "hdrsecret") || !strings.Contains(body, `"urlHint":"https://hooks.example.org"`) {
		t.Fatalf("save: %d %s", code, body)
	}
	if code, body = rpc("notify.list", `{}`); code != 200 || strings.Contains(body, "SECRETPART") || strings.Contains(body, "hdrsecret") {
		t.Fatalf("list: %d %s", code, body)
	}
	fi, err := os.Stat(filepath.Join(state, "notify.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("channels file: %v %v", err, fi)
	}
	// A plugin that does not exist cannot notify.
	if code, body = rpc("plugins.notify", `{"plugin":"nosuch","title":"x"}`); code != 404 {
		t.Fatalf("plugins.notify: %d %s", code, body)
	}
}

func TestDaemonAdminMethodsAskForUnlock(t *testing.T) {
	// A session that is not root, not unlocked and not --dev-insecure-noauth gets needs_admin.
	ts, srv, _ := newJobsServer(t, true)
	c := noAuthClient(t, srv, ts)
	srv.opts.NoAuth = false // from now on the session counts as locked
	code, body := doc(t, c, "POST", ts.URL+"/api/rpc", `{"method":"notify.list","params":{}}`, true)
	if code != 403 || !strings.Contains(body, "needs_admin") {
		t.Fatalf("%d %s", code, body)
	}
	if code, body = doc(t, c, "POST", ts.URL+"/api/rpc", `{"method":"jobs.list","params":{}}`, true); code != 403 || !strings.Contains(body, "needs_admin") {
		t.Fatalf("%d %s", code, body)
	}
}
