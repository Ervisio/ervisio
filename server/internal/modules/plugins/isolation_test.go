package plugins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configmod "github.com/Fonlogen/LinuxAdmin/server/internal/modules/config"
	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// Server-side enforcement of the plugin manifest (security review H2): the
// broker in the web app is a convenience; these checks are what holds.

func call(t *testing.T, admin bool, params any) *rpc.Call {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return &rpc.Call{Params: raw, Admin: admin}
}

const filesManifest = `{
  "id": "files", "name": "Files demo", "version": "1.0.0", "entry": "index.js",
  "capabilities": {
    "commands": [{"name": "ps", "argv": ["echo", "ps"], "admin": false}],
    "files": {"read": ["%READ%"], "write": ["%WRITE%"]},
    "sockets": [], "network": []
  },
  "contributes": {"pages": [], "widgets": [], "snippets": []},
  "visibleTo": {"groups": []}
}`

func setupFiles(t *testing.T) (readDir, writeDir, outside string) {
	t.Helper()
	system, _ := setup(t)
	base := t.TempDir()
	readDir = filepath.Join(base, "ro")
	writeDir = filepath.Join(base, "rw")
	outside = filepath.Join(base, "outside")
	for _, d := range []string{readDir, writeDir, outside, filepath.Join(readDir, "sub")} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(readDir, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(readDir, "bin.dat"), []byte{0xff, 0x00, 0xfe}, 0o644)
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644)
	os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(readDir, "link.txt"))
	os.Symlink(outside, filepath.Join(writeDir, "escape"))
	m := strings.ReplaceAll(strings.ReplaceAll(filesManifest, "%READ%", readDir), "%WRITE%", writeDir)
	writePlugin(t, filepath.Join(system, "files"), m, map[string]string{"index.js": "x"})
	return
}

func TestPluginReadFile(t *testing.T) {
	ro, rw, outside := setupFiles(t)
	read := func(path string) (*FileResult, error) {
		r, err := readPluginFile(context.Background(), call(t, false, FileParams{Plugin: "files", Path: path}))
		if err != nil {
			return nil, err
		}
		return r.(*FileResult), nil
	}
	if r, err := read(filepath.Join(ro, "a.txt")); err != nil || r.Data != "hello" || r.B64 {
		t.Fatalf("declared read: %+v %v", r, err)
	}
	if r, err := read(filepath.Join(ro, "bin.dat")); err != nil || !r.B64 || r.Data != "/wD+" {
		t.Fatalf("binary read: %+v %v", r, err)
	}
	os.WriteFile(filepath.Join(rw, "w.txt"), []byte("w"), 0o644)
	if _, err := read(filepath.Join(rw, "w.txt")); err != nil {
		t.Fatalf("write folders are readable too: %v", err)
	}
	for _, bad := range []string{
		filepath.Join(outside, "secret.txt"),             // undeclared
		filepath.Join(ro, "link.txt"),                    // symlink out of the folder
		filepath.Join(ro, "..", "outside", "secret.txt"), // .. out of the folder
		filepath.Join(rw, "escape", "secret.txt"),        // symlinked dir out of the folder
		"/etc/passwd",
		"relative.txt",
		"",
	} {
		if r, err := read(bad); err == nil {
			t.Errorf("%q was read: %+v", bad, r)
		}
	}
	if _, err := read(filepath.Join(outside, "secret.txt")); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("undeclared path must be forbidden, got %v", err)
	}
	// Never on the root bridge.
	if _, err := readPluginFile(context.Background(), call(t, true, FileParams{Plugin: "files", Path: filepath.Join(ro, "a.txt")})); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("admin file read: %v", err)
	}
	// Disabled plugin: nothing.
	st := readState()
	st.Enabled["files"] = false
	writeState(st)
	if _, err := read(filepath.Join(ro, "a.txt")); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("disabled plugin read a file: %v", err)
	}
	// ...and the daemon serves none of its assets or frames (plugins.access).
	if _, err := access(context.Background(), call(t, false, map[string]string{"id": "files"})); !rpc.IsCode(err, rpc.NotFound) {
		t.Errorf("access to a disabled plugin: %v", err)
	}
}

func TestPluginWriteAndList(t *testing.T) {
	ro, rw, outside := setupFiles(t)
	write := func(path, data string) error {
		_, err := writePluginFile(context.Background(), call(t, false, FileParams{Plugin: "files", Path: path, Data: data}))
		return err
	}
	if err := write(filepath.Join(rw, "new.txt"), "one"); err != nil {
		t.Fatal(err)
	}
	if err := write(filepath.Join(rw, "new.txt"), "two"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(rw, "new.txt")); string(b) != "two" {
		t.Fatalf("content %q", b)
	}
	if left, _ := filepath.Glob(filepath.Join(rw, ".la-plugin-*")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
	// A symlink at the target is replaced, never followed.
	os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(rw, "victim"))
	_ = write(filepath.Join(rw, "victim"), "pwned")
	if b, _ := os.ReadFile(filepath.Join(outside, "secret.txt")); string(b) != "secret" {
		t.Fatal("write followed a symlink out of the folder")
	}
	for _, bad := range []string{
		filepath.Join(ro, "a.txt"),                  // read-only capability
		filepath.Join(outside, "x.txt"),             // undeclared
		filepath.Join(rw, "escape", "x.txt"),        // through a symlinked dir
		filepath.Join(rw, "..", "outside", "x.txt"), // through ..
		rw, // the folder itself
	} {
		if err := write(bad, "x"); err == nil {
			t.Errorf("%q was written", bad)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "x.txt")); err == nil {
		t.Fatal("a file was created outside the declared folder")
	}
	if b, _ := os.ReadFile(filepath.Join(ro, "a.txt")); string(b) != "hello" {
		t.Fatal("read-only file changed")
	}

	r, err := listPluginDir(context.Background(), call(t, false, FileParams{Plugin: "files", Path: ro}))
	if err != nil {
		t.Fatal(err)
	}
	ents := r.(map[string]any)["entries"].([]DirEntry)
	names := []string{}
	for _, e := range ents {
		names = append(names, e.Name+":"+e.Type)
	}
	if strings.Join(names, ",") != "a.txt:file,bin.dat:file,link.txt:link,sub:dir" {
		t.Fatalf("listing %v", names)
	}
	if _, err := listPluginDir(context.Background(), call(t, false, FileParams{Plugin: "files", Path: outside})); err == nil {
		t.Fatal("undeclared folder listed")
	}
}

func TestMatchCapLongestPrefix(t *testing.T) {
	r, ok := matchCap([]string{"/srv", "/srv/www"}, "/srv/www/a")
	if !ok || r.dir != "/srv/www" || r.rel != "a" {
		t.Fatalf("%+v", r)
	}
	if _, ok := matchCap([]string{"/srv"}, "/srvx/a"); ok {
		t.Fatal("prefix without a separator matched")
	}
	if r, ok := matchCap([]string{"/"}, "/etc/x"); !ok || r.rel != "etc/x" {
		t.Fatalf("%+v", r)
	}
}

// Commands declared user-level never run on the root bridge, even if asked.
func TestUserCommandNotOnAdminBridge(t *testing.T) {
	system, _ := setup(t)
	writePlugin(t, filepath.Join(system, "demo"), strings.Replace(goodManifest, `"visibleTo": {"groups": ["docker"]}`, `"visibleTo": {"groups": []}`, 1), map[string]string{"index.js": "x"})
	if _, err := resolve(&rpc.Call{Admin: true}, ExecParams{Plugin: "demo", Command: "ps"}); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("user command on the root bridge: %v", err)
	}
}

// Default policy: only signed plugins run; unsigned dev folders still run in
// developer mode and are marked.
func TestTrustPolicy(t *testing.T) {
	system, _ := setup(t)
	os.Remove(configmod.Path) // built-in defaults
	dev := t.TempDir()
	DevDirs = []string{dev}
	visible := strings.Replace(goodManifest, `"visibleTo": {"groups": ["docker"]}`, `"visibleTo": {"groups": []}`, 1)
	writePlugin(t, filepath.Join(system, "demo"), visible, map[string]string{"index.js": "x"})
	writePlugin(t, filepath.Join(dev, "devdemo"), strings.Replace(visible, `"id": "demo"`, `"id": "devdemo"`, 1), map[string]string{"index.js": "x"})
	got := map[string]Info{}
	for _, in := range list(false) {
		got[in.ID] = in
	}
	if d := got["demo"]; !d.Blocked || d.Enabled {
		t.Fatalf("unsigned system plugin must be blocked by default: %+v", d)
	}
	if d := got["devdemo"]; d.Blocked || !d.Enabled || !d.DevUnsigned {
		t.Fatalf("unsigned dev plugin must run marked as such: %+v", d)
	}
	if _, err := resolve(&rpc.Call{}, ExecParams{Plugin: "demo", Command: "ps"}); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("blocked plugin ran: %v", err)
	}
	if _, err := resolve(&rpc.Call{}, ExecParams{Plugin: "devdemo", Command: "ps"}); err != nil {
		t.Fatalf("dev plugin: %v", err)
	}
	// plugins.access mirrors it: the daemon serves nothing of a blocked plugin.
	if _, err := access(context.Background(), call(t, false, map[string]string{"id": "demo"})); !rpc.IsCode(err, rpc.NotFound) {
		t.Fatalf("access to a blocked plugin: %v", err)
	}
	r, err := access(context.Background(), call(t, false, map[string]string{"id": "devdemo"}))
	if err != nil || r.(AccessInfo).ID != "devdemo" {
		t.Fatalf("access: %+v %v", r, err)
	}
}

func TestNetworkHosts(t *testing.T) {
	for _, ok := range []string{"example.org", "*.example.org", "localhost:9090", "a-b.c"} {
		if !NetworkHostRe.MatchString(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "x; script-src *", "'self'", "https://a.b", "a b", "*", "a/b", "EXAMPLE.org\n"} {
		if NetworkHostRe.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	m := strings.Replace(goodManifest, `"sockets": ["/var/run/docker.sock"]`, `"sockets": ["/var/run/docker.sock"], "network": ["x; script-src *"]`, 1)
	if _, err := ParseManifest([]byte(m)); err == nil {
		t.Fatal("CSP injection through capabilities.network accepted")
	}
}

// The shipped Docker example must verify with the embedded team key.
func TestShippedDockerSigned(t *testing.T) {
	dir := "../../../../plugins/docker"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("plugins/docker not found")
	}
	s := CheckSignature(dir, TrustedKeys)
	if !s.Signed || !s.Verified {
		t.Fatalf("plugins/docker: signed=%v verified=%v %s", s.Signed, s.Verified, s.Err)
	}
}
