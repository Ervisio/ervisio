package plugins

import (
	"context"
	"os"
	"path/filepath"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"strings"
	"testing"

	"github.com/ervisio/ervisio/server/internal/brand"
)

func TestCoreRequirement(t *testing.T) {
	old, oldDev := brand.Version, DaemonDev
	t.Cleanup(func() { brand.Version, DaemonDev = old, oldDev })
	withReq := func(extra string) string {
		return strings.Replace(goodManifest, `"entry": "index.js",`, `"entry": "index.js", `+extra+`,`, 1)
	}
	for _, c := range []struct{ doc, core, dev string }{
		{`"minCore": "0.5.0"`, "0.4.0", ""},
		{`"requires": {"ervisio": ">=0.5.0"}`, "0.4.9", ""},
		{`"minCore": "0.3.0", "requires": {"ervisio": ">=1.0.0"}`, "0.9.9", ""}, // the higher one counts
		{`"minCore": "0.5.0"`, "v0.4.0-33-gabc1234", "no"},                      // a dev build, but not a --dev daemon
	} {
		brand.Version, DaemonDev = c.core, c.dev == ""
		if c.dev == "no" {
			DaemonDev = false
		}
		m, err := ParseManifest([]byte(withReq(c.doc)))
		if err != nil {
			t.Fatalf("%s: %v", c.doc, err)
		}
		msg := m.CoreProblem()
		if !strings.Contains(msg, "needs Ervisio") || !strings.Contains(msg, "Update Ervisio") {
			t.Errorf("%s on %s: %q", c.doc, c.core, msg)
		}
	}
	ok := []struct{ doc, core string }{
		{`"minCore": "0.5.0"`, "0.5.0"},
		{`"minCore": "0.5.0"`, "v0.5.1"},
		{`"requires": {"ervisio": ">=0.5.0"}`, "1.0.0"},
		{``, "0.0.1"},
	}
	for _, c := range ok {
		brand.Version, DaemonDev = c.core, false
		doc := goodManifest
		if c.doc != "" {
			doc = withReq(c.doc)
		}
		m, err := ParseManifest([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		if msg := m.CoreProblem(); msg != "" {
			t.Errorf("%s on %s: %q", c.doc, c.core, msg)
		}
	}
	// A --dev daemon built from a checkout (v0.4.0-N-g...) runs plugins for the next release.
	brand.Version, DaemonDev = "v0.4.0-33-gabc1234", true
	m, _ := ParseManifest([]byte(withReq(`"minCore": "0.5.0"`)))
	if m.CoreProblem() != "" {
		t.Error("a development daemon refused a plugin for the next release")
	}
	// Bad spellings are refused when the manifest is read.
	for _, bad := range []string{`"minCore": "latest"`, `"minCore": "0.5"`, `"requires": {"ervisio": "^0.5.0"}`, `"requires": {"ervisio": ">=0.5.0", "x": 1}`} {
		if _, err := ParseManifest([]byte(withReq(bad))); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestCoreRequirementEnforced(t *testing.T) {
	old, oldDev := brand.Version, DaemonDev
	t.Cleanup(func() { brand.Version, DaemonDev = old, oldDev })
	brand.Version, DaemonDev = "0.4.0", false
	e := catalogEntryNeeding("0.5.0")
	if highest, _ := highestCoreReq(e.MinCore, e.Requires.get()); highest != "0.5.0" || coreProblem(e.Name, highest) == "" {
		t.Fatal("a catalog entry that needs a newer core was not flagged")
	}
}

func catalogEntryNeeding(v string) CatalogEntry {
	return CatalogEntry{ID: "x1", Name: "X", Version: "1.0.0", Requires: &Requires{Ervisio: ">=" + v}}
}

func TestInstallRefusesAPluginForANewerCore(t *testing.T) {
	_, installed := setup(t)
	old, oldDev := brand.Version, DaemonDev
	t.Cleanup(func() { brand.Version, DaemonDev = old, oldDev })
	brand.Version, DaemonDev = "0.4.0", false
	doc := strings.Replace(goodManifest, `"entry": "index.js",`, `"entry": "index.js", "minCore": "0.5.0",`, 1)
	arch := filepath.Join(t.TempDir(), "demo.tar.gz")
	os.WriteFile(arch, makeTar(t, []tent{{name: "demo/manifest.json", body: doc}, {name: "demo/index.js", body: "export default () => {}"}}), 0o644)
	_, err := install(context.Background(), installRequest{Source: arch})
	if !rpc.IsCode(err, rpc.Conflict) || !strings.Contains(err.Error(), "needs Ervisio 0.5.0 or newer") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(installed, "demo")); err == nil {
		t.Fatal("a refused install left files behind")
	}
	brand.Version = "0.5.0"
	if _, err := install(context.Background(), installRequest{Source: arch}); err != nil {
		t.Fatal(err)
	}
	// After a downgrade of the core the plugin is listed as incompatible, off, and refused.
	brand.Version = "0.4.0"
	var got *Info
	for _, in := range list(true) {
		if in.ID == "demo" {
			in := in
			got = &in
		}
	}
	if got == nil || got.Incompatible == "" || got.Enabled {
		t.Fatalf("%+v", got)
	}
	if _, _, err := authorize(&rpc.Call{}, "demo"); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("authorize: %v", err)
	}
	if _, err := Resolve("demo"); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("resolve: %v", err)
	}
}
