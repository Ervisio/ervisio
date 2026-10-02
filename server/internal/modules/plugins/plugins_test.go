package plugins

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

const goodManifest = `{
  "id": "demo", "name": "Demo", "version": "1.2.3", "author": "me", "entry": "index.js",
  "icon": "server", "color": "file",
  "capabilities": {
    "commands": [
      {"name": "ps", "argv": ["echo", "ps"], "admin": false},
      {"name": "stop", "argv": ["echo", "stop", "{0}"], "args": [{"pattern": "[a-z][a-z0-9_.-]*"}], "admin": true, "adminUnlessGroup": "docker"}
    ],
    "files": {"read": ["/srv", "~/projects"], "write": []},
    "sockets": ["/var/run/docker.sock"]
  },
  "contributes": {"pages": [{"id": "demo", "title": "Demo"}], "widgets": [], "snippets": [{"name": "x", "command": "echo x"}]},
  "visibleTo": {"groups": ["docker"]}
}`

func writePlugin(t *testing.T, dir, manifest string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for n, c := range files {
		p := filepath.Join(dir, n)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseManifestGood(t *testing.T) {
	m, err := ParseManifest([]byte(goodManifest))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "demo" || len(m.Capabilities.Commands) != 2 || !m.RunsRoot() {
		t.Fatalf("unexpected manifest %+v", m)
	}
	if m.Contributes.Widgets == nil || m.Capabilities.Network == nil {
		t.Fatal("nil slices must be normalised to empty")
	}
}

func TestParseManifestBad(t *testing.T) {
	mut := func(from, to string) string { return strings.Replace(goodManifest, from, to, 1) }
	cases := map[string]string{
		"id uppercase":      mut(`"id": "demo"`, `"id": "Demo"`),
		"id traversal":      mut(`"id": "demo"`, `"id": "../x"`),
		"bad version":       mut(`1.2.3`, `1.2`),
		"entry escapes":     mut(`"index.js"`, `"../index.js"`),
		"entry absolute":    mut(`"index.js"`, `"/etc/passwd"`),
		"entry not js":      mut(`"index.js"`, `"index.sh"`),
		"unknown field":     mut(`"author"`, `"extra": 1, "author"`),
		"bad color":         mut(`"file"`, `"pink"`),
		"slot undeclared":   mut(`"stop", "{0}"]`, `"stop", "{1}"]`),
		"unused arg":        mut(`["echo", "ps"]`, `["echo", "ps", "{0}"]`),
		"slot as program":   mut(`["echo", "stop", "{0}"]`, `["{0}", "stop"]`),
		"bad regex":         mut(`[a-z][a-z0-9_.-]*`, `[a-z`),
		"dup command":       mut(`"name": "stop"`, `"name": "ps"`),
		"unless w/o admin":  mut(`"admin": false`, `"admin": false, "adminUnlessGroup": "docker"`),
		"relative file cap": mut(`"/srv"`, `"srv"`),
		"socket traversal":  mut(`/var/run/docker.sock`, `/var/run/../docker.sock`),
		"bad group":         mut(`"docker"]}`, `"Dock er"]}`),
		"not json":          `{`,
		"trailing":          goodManifest + `{}`,
	}
	for name, src := range cases {
		if _, err := ParseManifest([]byte(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadManifestEntryInside(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	writePlugin(t, dir, goodManifest, nil)
	if _, err := LoadManifest(dir); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing entry: %v", err)
	}
	outside := filepath.Join(root, "outside.js")
	os.WriteFile(outside, []byte("x"), 0o644)
	os.Symlink(outside, filepath.Join(dir, "index.js"))
	if _, err := LoadManifest(dir); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("symlinked entry must be refused: %v", err)
	}
	os.Remove(filepath.Join(dir, "index.js"))
	os.WriteFile(filepath.Join(dir, "index.js"), []byte("export default () => {}"), 0o644)
	if _, err := LoadManifest(dir); err != nil {
		t.Fatal(err)
	}
}

func TestSignatureRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := []ed25519.PublicKey{pub}
	dir := filepath.Join(t.TempDir(), "demo")
	writePlugin(t, dir, goodManifest, map[string]string{"index.js": "export default () => {}", "lib/a.js": "1"})

	if s := CheckSignature(dir, keys); s.Signed {
		t.Fatal("unsigned folder reported as signed")
	}
	if err := SignFolder(dir, priv); err != nil {
		t.Fatal(err)
	}
	if s := CheckSignature(dir, keys); !s.Verified {
		t.Fatalf("fresh signature should verify: %+v", s)
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if s := CheckSignature(dir, []ed25519.PublicKey{other}); s.Verified || !s.Signed {
		t.Fatalf("wrong key must not verify: %+v", s)
	}

	// Changed file.
	os.WriteFile(filepath.Join(dir, "lib/a.js"), []byte("2"), 0o644)
	if s := CheckSignature(dir, keys); s.Verified || !strings.Contains(s.Err, "changed after signing") {
		t.Fatalf("tampered file: %+v", s)
	}
	os.WriteFile(filepath.Join(dir, "lib/a.js"), []byte("1"), 0o644)
	// Extra unlisted file.
	os.WriteFile(filepath.Join(dir, "evil.js"), []byte("1"), 0o644)
	if s := CheckSignature(dir, keys); s.Verified || !strings.Contains(s.Err, "not listed") {
		t.Fatalf("extra file: %+v", s)
	}
	os.Remove(filepath.Join(dir, "evil.js"))
	// Tampered manifest (capabilities widened).
	b, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	os.WriteFile(filepath.Join(dir, "manifest.json"), bytes.Replace(b, []byte(`"admin": false`), []byte(`"admin": true`), 1), 0o644)
	if s := CheckSignature(dir, keys); s.Verified {
		t.Fatalf("tampered manifest verified: %+v", s)
	}
}

func TestCanonicalIgnoresFormatting(t *testing.T) {
	a, _ := Canonical([]byte(`{"b": 1, "a": {"y": [1, 2], "x": "<>"}}`))
	b, _ := Canonical([]byte("{\n\"a\":{\"x\":\"<>\",\"y\":[1,2]},\"b\":1}"))
	if string(a) != string(b) || string(a) != `{"a":{"x":"<>","y":[1,2]},"b":1}` {
		t.Fatalf("%s vs %s", a, b)
	}
}

func TestEmbeddedKeyIsValid(t *testing.T) {
	if len(TrustedKeys) != 1 || len(TrustedKeys[0]) != ed25519.PublicKeySize {
		t.Fatal("bad embedded key")
	}
}

// ---- tar extraction ----

type tent struct {
	name string
	typ  byte
	body string
	link string
	mode int64
}

func makeTar(t *testing.T, ents []tent) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range ents {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: mode, Linkname: e.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestExtractRejectsUnsafe(t *testing.T) {
	bad := map[string][]tent{
		"dotdot":        {{name: "../evil.js", body: "x"}},
		"nested dotdot": {{name: "a/../../evil.js", body: "x"}},
		"absolute":      {{name: "/etc/evil", body: "x"}},
		"symlink":       {{name: "link", typ: tar.TypeSymlink, link: "/etc/passwd"}},
		"hardlink":      {{name: "hl", typ: tar.TypeLink, link: "manifest.json"}},
		"backslash":     {{name: `a\..\b`, body: "x"}},
		"fifo":          {{name: "f", typ: tar.TypeFifo}},
		"duplicate":     {{name: "a.js", body: "1"}, {name: "a.js", body: "2"}},
	}
	for name, ents := range bad {
		dest := t.TempDir()
		if err := ExtractTarGz(bytes.NewReader(makeTar(t, ents)), dest); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "evil.js")); err == nil {
			t.Errorf("%s: escaped the destination", name)
		}
	}
	if err := ExtractTarGz(strings.NewReader("not gzip"), t.TempDir()); err == nil {
		t.Error("garbage accepted")
	}
}

func TestExtractLimitsAndStrip(t *testing.T) {
	dest := t.TempDir()
	tarb := makeTar(t, []tent{
		{name: "demo/", typ: tar.TypeDir},
		{name: "demo/manifest.json", body: "{}"},
		{name: "demo/lib/a.js", body: "a"},
		{name: "demo/bin/run", body: "#!/bin/sh", mode: 0o755},
	})
	if err := ExtractTarGz(bytes.NewReader(tarb), dest); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"manifest.json", "lib/a.js", "bin/run"} {
		if _, err := os.Stat(filepath.Join(dest, f)); err != nil {
			t.Errorf("%s not extracted: %v", f, err)
		}
	}
	if fi, _ := os.Stat(filepath.Join(dest, "bin/run")); fi.Mode().Perm() != 0o755 {
		t.Errorf("exec bit lost: %v", fi.Mode())
	}
	// Flat archive stays flat.
	flat := t.TempDir()
	if err := ExtractTarGz(bytes.NewReader(makeTar(t, []tent{{name: "manifest.json", body: "{}"}, {name: "index.js", body: "x"}})), flat); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(flat, "index.js")); err != nil {
		t.Error("flat archive broken")
	}
	// Too big a file.
	big := strings.Repeat("a", maxFileBytes+1)
	if err := ExtractTarGz(bytes.NewReader(makeTar(t, []tent{{name: "big", body: big}})), t.TempDir()); err == nil {
		t.Error("oversized file accepted")
	}
}

// ---- exec ----

func TestSubstitute(t *testing.T) {
	m, err := ParseManifest([]byte(goodManifest))
	if err != nil {
		t.Fatal(err)
	}
	stop := &m.Capabilities.Commands[1]
	argv, err := Substitute(stop, []string{"web-1"})
	if err != nil || strings.Join(argv, " ") != "echo stop web-1" {
		t.Fatalf("%v %v", argv, err)
	}
	for _, bad := range []string{"-rf", "a b", "UPPER", "a;b", "../x", "", "x\nrm", strings.Repeat("a", 300)} {
		if _, err := Substitute(stop, []string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := Substitute(stop, nil); err == nil {
		t.Error("missing argument accepted")
	}
	if _, err := Substitute(stop, []string{"a", "b"}); err == nil {
		t.Error("extra argument accepted")
	}
	// Embedded slot and repeated slot.
	c := &Command{Name: "x", Argv: []string{"prog", "--name={0}", "{0}"}, Args: []ArgSpec{{Pattern: `\w+`}}}
	got, err := Substitute(c, []string{"abc"})
	if err != nil || strings.Join(got, "|") != "prog|--name=abc|abc" {
		t.Fatalf("%v %v", got, err)
	}
	// Template braces without digits are left alone.
	c = &Command{Name: "x", Argv: []string{"docker", "ps", "--format", "{{json .}}"}}
	got, _ = Substitute(c, nil)
	if got[3] != "{{json .}}" {
		t.Fatalf("%v", got)
	}
	// The pattern must match the whole value.
	c = &Command{Name: "x", Argv: []string{"p", "{0}"}, Args: []ArgSpec{{Pattern: `a|b`}}}
	if _, err := Substitute(c, []string{"ab"}); err == nil {
		t.Error("partial match accepted")
	}
}

func setup(t *testing.T) (system, installed string) {
	t.Helper()
	root := t.TempDir()
	SystemDir = filepath.Join(root, "share")
	InstalledDir = filepath.Join(root, "lib")
	StatePath = filepath.Join(root, "state.json")
	CatalogURLFile = filepath.Join(root, "none.url")
	DevDirs = []string{}
	configmod.Path = filepath.Join(root, "ervisio.conf")
	os.MkdirAll(SystemDir, 0o755)
	os.MkdirAll(InstalledDir, 0o755)
	// Most tests use unsigned fixtures; the default (signed only) is tested in TestTrustPolicy.
	os.WriteFile(configmod.Path, []byte("[plugins]\nallow_unsigned = true\n"), 0o644)
	t.Cleanup(func() { DevDirs = nil })
	return SystemDir, InstalledDir
}

func TestListAndEnable(t *testing.T) {
	// list(true) = admin caller: broken plugins are only listed to admins, and the
	// test user's own groups (wheel/sudo locally, not on CI runners) must not matter.
	system, _ := setup(t)
	writePlugin(t, filepath.Join(system, "demo"), strings.Replace(goodManifest, `"visibleTo": {"groups": ["docker"]}`, `"visibleTo": {"groups": []}`, 1), map[string]string{"index.js": "x"})
	writePlugin(t, filepath.Join(system, "broken"), `{"id": "broken"}`, nil)
	l := list(true)
	if len(l) != 2 {
		t.Fatalf("%+v", l)
	}
	var demo *Info
	for i := range l {
		if l[i].ID == "demo" {
			demo = &l[i]
		}
	}
	if demo == nil || !demo.Enabled || demo.Signed || demo.Location != LocSystem || demo.Removable {
		t.Fatalf("%+v", demo)
	}
	st := readState()
	st.Enabled["demo"] = false
	if err := writeState(st); err != nil {
		t.Fatal(err)
	}
	for _, in := range list(true) {
		if in.ID == "demo" && in.Enabled {
			t.Fatal("still enabled after disabling")
		}
	}
	// unsigned not allowed
	os.WriteFile(configmod.Path, []byte("[plugins]\nallow_unsigned = false\n"), 0o644)
	for _, in := range list(true) {
		if in.ID == "demo" && (!in.Blocked || in.Enabled) {
			t.Fatalf("unsigned plugin should be blocked: %+v", in)
		}
	}
}

func TestVisibility(t *testing.T) {
	system, _ := setup(t)
	writePlugin(t, filepath.Join(system, "demo"), strings.Replace(goodManifest, `["docker"]`, `["no-such-group-xyz"]`, 1), map[string]string{"index.js": "x"})
	if os.Geteuid() == 0 {
		t.Skip("root sees everything")
	}
	who := currentCaller(false)
	if who.Admin {
		t.Skip("test user is in an admin group")
	}
	if len(list(false)) != 0 {
		t.Fatal("plugin restricted to another group must be hidden")
	}
	if len(list(true)) != 1 {
		t.Fatal("the root bridge sees everything")
	}
}

func TestResolveLevels(t *testing.T) {
	system, _ := setup(t)
	writePlugin(t, filepath.Join(system, "demo"), strings.Replace(goodManifest, `"visibleTo": {"groups": ["docker"]}`, `"visibleTo": {"groups": []}`, 1), map[string]string{"index.js": "x"})
	user := &rpc.Call{}
	if _, err := resolve(user, ExecParams{Plugin: "demo", Command: "ps"}, false); err != nil {
		t.Fatalf("user command: %v", err)
	}
	if os.Geteuid() != 0 && !currentCaller(false).Groups["docker"] {
		if _, err := resolve(user, ExecParams{Plugin: "demo", Command: "stop", Args: []string{"x"}}, false); !rpc.IsCode(err, rpc.NeedsAdmin) {
			t.Fatalf("admin command from the user bridge must ask for admin, got %v", err)
		}
	}
	if _, err := resolve(&rpc.Call{Admin: true}, ExecParams{Plugin: "demo", Command: "stop", Args: []string{"x"}}, false); err != nil {
		t.Fatalf("admin bridge: %v", err)
	}
	if _, err := resolve(user, ExecParams{Plugin: "demo", Command: "nope"}, false); !rpc.IsCode(err, rpc.NotFound) {
		t.Fatalf("unknown command: %v", err)
	}
	if _, err := resolve(user, ExecParams{Plugin: "ghost", Command: "ps"}, false); !rpc.IsCode(err, rpc.NotFound) {
		t.Fatalf("unknown plugin: %v", err)
	}
	if _, err := resolve(&rpc.Call{Admin: true}, ExecParams{Plugin: "demo", Command: "stop", Args: []string{"-bad"}}, false); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatalf("bad arg: %v", err)
	}
	st := readState()
	st.Enabled["demo"] = false
	writeState(st)
	if _, err := resolve(user, ExecParams{Plugin: "demo", Command: "ps"}, false); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("disabled plugin must not run: %v", err)
	}
}

func TestRunExec(t *testing.T) {
	system, _ := setup(t)
	m := strings.Replace(goodManifest, `"visibleTo": {"groups": ["docker"]}`, `"visibleTo": {"groups": []}`, 1)
	m = strings.Replace(m, `{"name": "ps", "argv": ["echo", "ps"], "admin": false},`, `{"name": "ps", "argv": ["echo", "ps"], "admin": false}, {"name": "fail", "argv": ["sh", "-c", "echo out; echo err >&2; exit 3"], "admin": false},`, 1)
	writePlugin(t, filepath.Join(system, "demo"), m, map[string]string{"index.js": "x"})
	res, err := runExec(context.Background(), &rpc.Call{}, ExecParams{Plugin: "demo", Command: "ps"})
	if err != nil || strings.TrimSpace(res.Stdout) != "ps" || res.ExitCode != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	res, err = runExec(context.Background(), &rpc.Call{}, ExecParams{Plugin: "demo", Command: "fail"})
	if err != nil || res.ExitCode != 3 || strings.TrimSpace(res.Stdout) != "out" || strings.TrimSpace(res.Stderr) != "err" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestInstallUninstall(t *testing.T) {
	_, installed := setup(t)
	files := []tent{
		{name: "demo/manifest.json", body: strings.Replace(goodManifest, `"visibleTo": {"groups": ["docker"]}`, `"visibleTo": {"groups": []}`, 1)},
		{name: "demo/index.js", body: "export default () => {}"},
	}
	arch := filepath.Join(t.TempDir(), "demo.tar.gz")
	os.WriteFile(arch, makeTar(t, files), 0o644)
	var consent Capabilities
	m, _ := ParseManifest([]byte(goodManifest))
	consent = m.Capabilities
	consent.Sockets = nil // not what the package declares
	if _, err := install(context.Background(), installRequest{Source: arch, Consent: &consent}); !rpc.IsCode(err, rpc.Conflict) {
		t.Fatalf("mismatching consent must be refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installed, "demo")); err == nil {
		t.Fatal("refused install left files behind")
	}
	in, err := install(context.Background(), installRequest{Source: arch, Consent: &m.Capabilities})
	if err != nil || in.ID != "demo" || in.Location != LocInstalled || !in.Enabled || !in.Removable {
		t.Fatalf("%+v %v", in, err)
	}
	// reinstall = update
	if _, err := install(context.Background(), installRequest{Source: arch}); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(installed)
	if len(ents) != 1 {
		t.Fatalf("temp files left in %s: %v", installed, ents)
	}
	// strict mode refuses unsigned
	os.WriteFile(configmod.Path, []byte("[plugins]\nallow_unsigned = false\n"), 0o644)
	os.RemoveAll(filepath.Join(installed, "demo"))
	if _, err := install(context.Background(), installRequest{Source: arch}); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("unsigned install must be refused: %v", err)
	}
	os.WriteFile(configmod.Path, []byte("[plugins]\nallow_unsigned = true\n"), 0o644)
	// bad checksum
	if _, err := install(context.Background(), installRequest{Source: arch, SHA256: strings.Repeat("0", 64)}); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatalf("checksum: %v", err)
	}
	// non-https URL, relative path
	if _, err := install(context.Background(), installRequest{Source: "http://example.com/x.tar.gz"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatalf("http: %v", err)
	}
	if _, err := install(context.Background(), installRequest{Source: "x.tar.gz"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatalf("relative: %v", err)
	}
	if _, err := install(context.Background(), installRequest{Source: arch}); err != nil {
		t.Fatal(err)
	}
	if err := uninstall("demo"); err != nil {
		t.Fatal(err)
	}
	if err := uninstall("demo"); !rpc.IsCode(err, rpc.NotFound) {
		t.Fatalf("second uninstall: %v", err)
	}
	if err := uninstall("../etc"); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatalf("traversal id: %v", err)
	}
}

func TestInstallSignedVerified(t *testing.T) {
	_, installed := setup(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	oldKeys := Keys
	Keys = func() []ed25519.PublicKey { return []ed25519.PublicKey{pub} }
	defer func() { Keys = oldKeys }()

	src := filepath.Join(t.TempDir(), "demo")
	writePlugin(t, src, strings.Replace(goodManifest, `"visibleTo": {"groups": ["docker"]}`, `"visibleTo": {"groups": []}`, 1), map[string]string{"index.js": "export default () => {}"})
	if err := SignFolder(src, priv); err != nil {
		t.Fatal(err)
	}
	pack := func(tamper bool) string {
		var ents []tent
		for _, n := range []string{"manifest.json", "manifest.sig", "index.js"} {
			b, _ := os.ReadFile(filepath.Join(src, n))
			if tamper && n == "index.js" {
				b = append(b, []byte("//evil")...)
			}
			ents = append(ents, tent{name: n, body: string(b)})
		}
		p := filepath.Join(t.TempDir(), "p.tar.gz")
		os.WriteFile(p, makeTar(t, ents), 0o644)
		return p
	}
	os.WriteFile(configmod.Path, []byte("[plugins]\nallow_unsigned = false\n"), 0o644)
	in, err := install(context.Background(), installRequest{Source: pack(false)})
	if err != nil || !in.Verified || !in.Signed {
		t.Fatalf("%+v %v", in, err)
	}
	os.RemoveAll(filepath.Join(installed, "demo"))
	if _, err := install(context.Background(), installRequest{Source: pack(true)}); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("tampered package must be refused: %v", err)
	}
}

func TestCatalogParse(t *testing.T) {
	c, err := parseCatalog([]byte(`{"plugins":[{"id":"ok","version":"1.0.0","name":"Ok"},{"id":"BAD","version":"1.0.0"},{"id":"ok","version":"2.0.0"},{"id":"nov","version":"x"}]}`))
	if err != nil || len(c.Plugins) != 1 || c.Plugins[0].Capabilities.Commands == nil {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestCompareSemver(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{{"1.5.0", "1.4.0", 1}, {"1.4.0", "1.4.0", 0}, {"1.4.0", "1.10.0", -1}, {"2.0.0-rc.1", "2.0.0", -1}, {"1.0.0+b", "1.0.0", 0}} {
		if got := compareSemver(tc.a, tc.b); got != tc.want {
			t.Errorf("%s vs %s = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestLoadDevNeedsDevMode(t *testing.T) {
	setup(t)
	DevDirs = nil
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(t.TempDir(), "demo")
	writePlugin(t, dir, goodManifest, map[string]string{"index.js": "x"})
	if _, err := loadDev(dir); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("dev off: %v", err)
	}
	os.WriteFile(configmod.Path, []byte("[plugins]\nallow_unsigned = true\ndev = true\n"), 0o644)
	info, err := loadDev(dir)
	if err != nil || info.ID != "demo" {
		t.Fatalf("%+v %v", info, err)
	}
	found := false
	for _, in := range list(true) {
		if in.ID == "demo" && in.Location == LocDev {
			found = true
		}
	}
	if !found {
		t.Fatal("loaded dev plugin not listed")
	}
	if _, err := unloadDev(dir); err != nil {
		t.Fatal(err)
	}
	if len(registeredDev()) != 0 {
		t.Fatal("not removed")
	}
	_ = json.Marshal
}

func TestCanSee(t *testing.T) {
	m, _ := ParseManifest([]byte(goodManifest)) // visibleTo docker
	if (caller{Groups: map[string]bool{"users": true}}).canSee(m) {
		t.Error("user outside the group must not see it")
	}
	if !(caller{Groups: map[string]bool{"docker": true}}).canSee(m) {
		t.Error("group member must see it")
	}
	if !(caller{Admin: true, Groups: map[string]bool{}}).canSee(m) {
		t.Error("admin sees everything")
	}
	m.VisibleTo.Groups = nil
	if !(caller{Groups: map[string]bool{}}).canSee(m) {
		t.Error("no restriction means everyone")
	}
}

// The sample catalog shipped in plugins/catalog.json must parse and hold valid entries.
func TestShippedCatalog(t *testing.T) {
	b, err := os.ReadFile("../../../../plugins/catalog.json")
	if err != nil {
		t.Skip("catalog not found")
	}
	var raw struct{ Plugins []json.RawMessage }
	json.Unmarshal(b, &raw)
	c, err := parseCatalog(b)
	if err != nil || len(c.Plugins) != len(raw.Plugins) || len(c.Plugins) != 6 {
		t.Fatalf("%d of %d entries valid: %v", len(c.Plugins), len(raw.Plugins), err)
	}
	for _, e := range c.Plugins {
		for i := range e.Capabilities.Commands {
			if err := e.Capabilities.Commands[i].validate(); err != nil {
				t.Errorf("%s: %v", e.ID, err)
			}
		}
	}
	// The docker entry must advertise the same permissions as the shipped plugin.
	m, _ := LoadManifest("../../../../plugins/docker")
	if m == nil || !sameJSON(c.find("docker").Capabilities, m.Capabilities) {
		t.Error("docker catalog entry differs from plugins/docker/manifest.json")
	}
}

type assetStream struct {
	events []json.RawMessage
	data   []byte
}

func (f *assetStream) Send(v any) error {
	b, _ := json.Marshal(v)
	f.events = append(f.events, b)
	return nil
}
func (f *assetStream) SendBytes(b []byte) error      { f.data = append(f.data, b...); return nil }
func (f *assetStream) Input() <-chan json.RawMessage { return nil }

func TestDevAsset(t *testing.T) {
	setup(t)
	DevDirs = nil
	t.Setenv("HOME", t.TempDir())
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("secret"), 0o644)
	dir := filepath.Join(t.TempDir(), "demo")
	writePlugin(t, dir, goodManifest, map[string]string{"index.js": "export default 1", "assets/a.css": "body{}"})
	os.Symlink(outside, filepath.Join(dir, "link.txt"))

	get := func(id, file string) (*assetStream, error) {
		raw, _ := json.Marshal(map[string]string{"id": id, "file": file})
		s := &assetStream{}
		return s, devAsset(context.Background(), &rpc.Call{Params: raw}, s)
	}
	// Not loaded yet / dev off.
	if _, err := get("demo", "index.js"); !rpc.IsCode(err, rpc.NotFound) {
		t.Fatalf("not loaded: %v", err)
	}
	DaemonDev = true
	t.Cleanup(func() { DaemonDev = false })
	if _, err := loadDev(dir); err != nil {
		t.Fatal(err)
	}
	s, err := get("demo", "assets/a.css")
	if err != nil || string(s.data) != "body{}" || len(s.events) != 1 {
		t.Fatalf("%v %q %d", err, s.data, len(s.events))
	}
	for _, bad := range []string{"../secret.txt", "/etc/passwd", "link.txt", "assets/../../x", "assets", ""} {
		if _, err := get("demo", bad); err == nil {
			t.Errorf("%q served", bad)
		}
	}
	if _, err := get("other", "index.js"); err == nil {
		t.Error("unknown id served")
	}
}
