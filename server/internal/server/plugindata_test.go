package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ervisio/ervisio/server/internal/modules/prefs"
)

func TestPrivateDataHTTPChecksPluginIdentityAndCAS(t *testing.T) {
	ts, srv, _ := newJobsServer(t, true)
	srv.opts.StateDir = privateStateTemp(t)
	root := t.TempDir()
	srv.opts.DevPluginsDir = root
	srv.opts.PluginDirs = []string{root}
	for _, id := range []string{"sample", "foreign"} {
		dir := filepath.Join(root, id)
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		manifest := `{"id":"` + id + `","name":"Example","version":"1.0.0","entry":"index.js","platforms":["linux","windows"],"capabilities":{},"contributes":{"pages":[{"id":"main","title":"Example"}]},"visibleTo":{"groups":[]}}`
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("export default()=>{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	client := noAuthClient(t, srv, ts)
	run := func(method string, params any) (int, map[string]json.RawMessage) {
		body, _ := json.Marshal(map[string]any{"method": method, "params": params})
		code, raw := doc(t, client, "POST", ts.URL+"/api/rpc", string(body), true)
		var out map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatal(err)
		}
		return code, out
	}
	code, created := run("plugins.data.write", map[string]any{"plugin": "sample", "key": "chat/one.json", "text": "private", "revision": ""})
	if code != 200 {
		_, detail := doc(t, client, "POST", ts.URL+"/api/rpc", `{"method":"plugins.list","params":{}}`, true)
		t.Fatalf("write: %d %s; plugin fixtures: %s", code, created, detail)
	}
	var version struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(created["result"], &version); err != nil {
		t.Fatal(err)
	}
	if code, _ = run("plugins.data.write", map[string]any{"plugin": "sample", "key": "chat/one.json", "text": "stale", "revision": ""}); code != 409 {
		t.Fatalf("stale overwrite: %d", code)
	}
	if code, _ = run("plugins.data.read", map[string]any{"plugin": "foreign", "key": "chat/one.json"}); code != 404 {
		t.Fatalf("plugin data mixed: %d", code)
	}
	if code, _ = run("plugins.data.write", map[string]any{"plugin": "missing", "key": "secret", "text": "blocked"}); code != 404 {
		t.Fatalf("uninstalled plugin: %d", code)
	}
	if code, _ = run("plugins.data.read", map[string]any{"plugin": "sample", "key": "../../outside"}); code != 400 {
		t.Fatalf("traversal: %d", code)
	}
	if code, _ = run("plugins.data.remove", map[string]any{"plugin": "sample", "key": "chat/one.json", "revision": version.Revision}); code != 200 {
		t.Fatalf("remove: %d", code)
	}
}

func TestPersonalPreferencesHTTPNeverNeedsRootOrTouchesHome(t *testing.T) {
	ts, srv, _ := newJobsServer(t, true)
	srv.opts.StateDir = privateStateTemp(t)
	dir, err := srv.accountStateDirectory(&Session{Account: srv.devUser})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prefs.NewStore(filepath.Join(dir, "prefs.json")).Update(map[string]json.RawMessage{}, true); err != nil {
		t.Fatal(err)
	}
	client := noAuthClient(t, srv, ts)
	code, body := doc(t, client, "POST", ts.URL+"/api/rpc", `{"method":"prefs.set","admin":true,"params":{"key":"shell.mode","value":"ervisios"}}`, true)
	if code != 200 || strings.Contains(body, "needs_admin") {
		t.Fatalf("personal setting asked root: %d %s", code, body)
	}
	code, body = doc(t, client, "POST", ts.URL+"/api/rpc", `{"method":"prefs.get","params":{}}`, true)
	if code != 200 || !strings.Contains(body, `"shell.mode":"ervisios"`) {
		t.Fatalf("preferences not persisted: %d %s", code, body)
	}
}
