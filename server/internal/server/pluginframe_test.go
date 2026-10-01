package server

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

const frameTestManifest = `{"id":"%ID%","name":"Demo","version":"1.0.0","entry":"index.js",
 "capabilities":{"commands":[],"files":{"read":[],"write":[]},"sockets":[],"network":["api.example.org"]},
 "contributes":{"pages":[{"id":"main","title":"Demo"}],"widgets":[],"snippets":[]},
 "visibleTo":{"groups":[%GROUPS%]}}`

func writeFramePlugin(t *testing.T, dir, id, groups string) {
	t.Helper()
	p := filepath.Join(dir, id)
	os.MkdirAll(p, 0o755)
	m := strings.ReplaceAll(strings.ReplaceAll(frameTestManifest, "%ID%", id), "%GROUPS%", groups)
	os.WriteFile(filepath.Join(p, "manifest.json"), []byte(m), 0o644)
	os.WriteFile(filepath.Join(p, "index.js"), []byte("export default () => {}"), 0o644)
	os.WriteFile(filepath.Join(p, "page.html"), []byte("<script>alert(1)</script>"), 0o644)
}

// The plugin frame and plugin assets are served only for plugins the
// session may use, with headers that keep plugin code out of the app origin.
func TestPluginFrameAndAssets(t *testing.T) {
	plugins := t.TempDir()
	writeFramePlugin(t, plugins, "demo", "")
	writeFramePlugin(t, plugins, "hidden", `"no-such-group-xyz"`)
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html>app"), 0o644)
	srv, err := New(Options{
		ConfigPath:    filepath.Join(t.TempDir(), "linuxadmin.conf"),
		Dev:           true,
		NoAuth:        true,
		WebDir:        web,
		Bridge:        buildBridge(t),
		PluginDirs:    []string{plugins},
		DevPluginsDir: plugins,
		Logger:        log.New(io.Discard, "", 0),
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
	cl := noAuthClient(t, srv, ts)
	get := func(path string) *http.Response {
		t.Helper()
		resp, err := cl.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp
	}

	// Frame of a usable plugin: sandboxed, nonce'd runtime, only declared network.
	resp := get("/plugin-frame/demo")
	if resp.StatusCode != 200 {
		t.Fatalf("frame: %d", resp.StatusCode)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{"sandbox allow-scripts", "default-src 'none'", "script-src 'nonce-", "connect-src https://api.example.org wss://api.example.org;", "frame-ancestors 'self'", "form-action 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("frame CSP lacks %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "allow-same-origin") || strings.Contains(csp, "'unsafe-eval'") {
		t.Errorf("frame CSP too loose: %s", csp)
	}
	if resp.Header.Get("X-Frame-Options") != "" {
		t.Error("the app must be able to frame /plugin-frame")
	}

	// Plugin assets: inert in the app origin.
	resp = get("/plugins/demo/page.html")
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") ||
		resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Fatalf("asset headers: %d %v", resp.StatusCode, resp.Header)
	}

	// Unknown plugins: nothing.
	for _, p := range []string{"/plugin-frame/ghost", "/plugins/ghost/index.js", "/plugin-frame/..", "/plugins/demo/../hidden/index.js"} {
		if resp := get(p); resp.StatusCode == 200 {
			t.Errorf("%s served", p)
		}
	}

	// A plugin whose visibleTo excludes the user is neither framed nor served
	// (security review L2). Admin-group members see everything.
	if os.Geteuid() != 0 && !inAdminGroup(t) {
		for _, p := range []string{"/plugin-frame/hidden", "/plugins/hidden/index.js"} {
			if resp := get(p); resp.StatusCode != 404 {
				t.Errorf("%s: %d, want 404", p, resp.StatusCode)
			}
		}
	}

	// Without a session nothing is served.
	for _, p := range []string{"/plugin-frame/demo", "/plugins/demo/index.js"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("%s without session: %d", p, resp.StatusCode)
		}
	}
}

func inAdminGroup(t *testing.T) bool {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	ids, _ := u.GroupIds()
	for _, id := range ids {
		if g, err := user.LookupGroupId(id); err == nil && (g.Name == "wheel" || g.Name == "sudo" || g.Name == "admin") {
			return true
		}
	}
	return false
}

func TestPluginFrameCSPRejectsInjection(t *testing.T) {
	csp := pluginFrameCSP("n", []string{"ok.example.org", "x; script-src *", "'self'", "https://a.b"})
	connect := csp[strings.Index(csp, "connect-src"):]
	connect = connect[:strings.Index(connect, ";")]
	if strings.Contains(csp, "script-src *") || strings.Contains(connect, "'self'") || strings.Contains(csp, "https://https") {
		t.Fatal(csp)
	}
	if !strings.Contains(csp, "connect-src https://ok.example.org wss://ok.example.org;") {
		t.Fatal(csp)
	}
	if !strings.Contains(pluginFrameCSP("n", nil), "connect-src 'none'") {
		t.Fatal("no network by default")
	}
}
