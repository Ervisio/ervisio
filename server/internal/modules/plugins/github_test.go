package plugins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
)

func TestParseRepoSpec(t *testing.T) {
	for _, ok := range []string{"", "  \n", "{}", `{"path": "."}`, `{"path": "apps/ui", "source": "branch", "branch": "release/2"}`} {
		if _, err := parseRepoSpec([]byte(ok)); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"nope", `{"source": "tag"}`, `{"path": "../x"}`, `{"path": "/etc"}`, `{"path": "a/../b"}`, `{"branch": "a b"}`} {
		if _, err := parseRepoSpec([]byte(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestIsUpdate(t *testing.T) {
	m := &Manifest{Version: "1.0.0"}
	e := &CatalogEntry{Repo: "acme/tool", Revision: "r2", Version: "1.0.0"}
	if !isUpdate(e, m, origin{Repo: "acme/tool", Revision: "r1"}) {
		t.Error("a new revision of the same repository is an update")
	}
	if isUpdate(e, m, origin{Repo: "acme/tool", Revision: "r2"}) {
		t.Error("the same revision is not an update")
	}
	if isUpdate(e, m, origin{}) {
		t.Error("a plugin installed from elsewhere only follows a higher version")
	}
	e.Version = "1.1.0"
	if !isUpdate(e, m, origin{}) {
		t.Error("a higher version is an update")
	}
}

// useGitHub points the marketplace at the fake organisation "acme" on m.
func useGitHub(t *testing.T, m *market) {
	t.Helper()
	githubAPI, githubRaw, githubWeb, githubCodeload = m.url("/api"), m.url("/raw"), m.url("/web"), m.url("/codeload")
	conf := "[plugins]\nallow_unsigned = false\ngithub_org = \"acme\"\n"
	if err := os.WriteFile(configmod.Path, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
}

func demoManifest(id, version string) string {
	return strings.NewReplacer(`"id": "demo"`, `"id": "`+id+`"`, `"version": "1.2.3"`, `"version": "`+version+`"`,
		`"visibleTo": {"groups": ["docker"]}`, `"visibleTo": {"groups": []}`).Replace(goodManifest)
}

// syncNow runs a pass that reads the organisation again.
func syncNow(t *testing.T) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(GitHubCatalogPath, old, old)
	syncGitHubOnce(context.Background(), t.Logf)
}

func installedFile(t *testing.T, id, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(InstalledDir, id, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGitHubMarketplace(t *testing.T) {
	setup(t)
	m := newMarket(t)
	useGitHub(t, m)

	repos := []githubRepo{
		{Name: "plugin-rel", DefaultBranch: "main", PushedAt: "t1"},
		{Name: "plugin-sub", DefaultBranch: "main", PushedAt: "t1"},
		{Name: "tool", DefaultBranch: "main", PushedAt: "t1"},
		{Name: "plugin-sdk", DefaultBranch: "main", PushedAt: "t1"},
		{Name: "plugin-old", DefaultBranch: "main", PushedAt: "t1", Archived: true},
		{Name: "website", DefaultBranch: "main", PushedAt: "t1"},
	}
	listRepos := func() {
		b, _ := json.Marshal(repos)
		m.put("/api/orgs/acme/repos", b)
	}
	listRepos()

	// plugin-rel: a release with its package attached.
	m.put("/raw/acme/plugin-rel/v1.0.0/plugin/manifest.json", []byte(demoManifest("rel", "1.0.0")))
	m.put("/web/acme/plugin-rel/releases/latest", []byte("/web/acme/plugin-rel/releases/tag/v1.0.0"))
	m.put("/web/acme/plugin-rel/releases/download/v1.0.0/rel-1.0.0.tar.gz", makeTar(t, []tent{
		{name: "rel/manifest.json", body: demoManifest("rel", "1.0.0")}, {name: "rel/index.js", body: "rel 1.0"},
	}))
	// Without a release GitHub redirects to the list of releases.
	for _, r := range []string{"plugin-sub", "tool", "plugin-sdk", "website"} {
		m.put("/web/acme/"+r+"/releases/latest", []byte("/web/acme/"+r+"/releases"))
	}
	// plugin-sub: no release, the plugin in the plugin folder of the branch.
	m.put("/raw/acme/plugin-sub/main/plugin/manifest.json", []byte(demoManifest("sub", "0.1.0")))
	m.put("/codeload/acme/plugin-sub/tar.gz/refs/heads/main", makeTar(t, []tent{
		{name: "plugin-sub-main/README.md", body: "readme"},
		{name: "plugin-sub-main/plugin/manifest.json", body: demoManifest("sub", "0.1.0")},
		{name: "plugin-sub-main/plugin/index.js", body: "sub"},
	}))
	// tool: a plugin by its plugin.ervisio, at the repository root.
	m.put("/raw/acme/tool/main/plugin.ervisio", []byte(`{"path": ".", "category": "Storage"}`))
	m.put("/raw/acme/tool/main/manifest.json", []byte(demoManifest("tool", "2.0.0")))
	toolPackage := func(body string) {
		m.put("/codeload/acme/tool/tar.gz/refs/heads/main", makeTar(t, []tent{
			{name: "tool-main/manifest.json", body: demoManifest("tool", "2.0.0")}, {name: "tool-main/index.js", body: body},
		}))
	}
	toolPackage("tool a")

	syncNow(t)
	cat := readGitHubCatalog(readPolicy())
	if cat == nil || len(cat.Plugins) != 3 {
		t.Fatalf("%+v", cat)
	}
	if e := cat.find("tool"); e == nil || e.Category != "Storage" || e.Repo != "acme/tool" || e.Revision != "t1" || e.Path != "" {
		t.Fatalf("tool: %+v", e)
	}
	if e := cat.find("rel"); e == nil || e.Revision != "v1.0.0" || !strings.HasSuffix(e.Source, "/v1.0.0/rel-1.0.0.tar.gz") {
		t.Fatalf("rel: %+v", e)
	}
	if e := cat.find("sub"); e == nil || e.Path != "plugin" {
		t.Fatalf("sub: %+v", e)
	}

	for _, id := range []string{"rel", "sub", "tool"} {
		if _, err := InstallFromCatalog(context.Background(), id); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(InstalledDir, "sub", "README.md")); err == nil {
		t.Error("files outside the plugin folder were installed")
	}
	// Unsigned, on a server that only runs signed plugins: trusted by where it came from.
	for _, in := range list(true) {
		if !in.Verified || in.Blocked || in.UpdateAvailable != nil {
			t.Errorf("%s: %+v", in.ID, in)
		}
	}
	if _, err := Resolve("tool"); err != nil {
		t.Error(err)
	}

	// A repository that did not change is not read again.
	before := m.hits["/raw/acme/tool/main/plugin.ervisio"]
	syncNow(t)
	if m.hits["/raw/acme/tool/main/plugin.ervisio"] != before {
		t.Error("an unchanged repository was read again")
	}

	// A push to the branch updates the plugin, with the same version.
	repos[2].PushedAt = "t2"
	listRepos()
	toolPackage("tool b")
	syncNow(t)
	if got := installedFile(t, "tool", "index.js"); got != "tool b" {
		t.Errorf("tool not updated: %q", got)
	}
	if o := readState().Origins["tool"]; o.Repo != "acme/tool" || o.Revision != "t2" {
		t.Errorf("%+v", o)
	}

	// A new release counts once its package is attached.
	m.put("/web/acme/plugin-rel/releases/latest", []byte("/web/acme/plugin-rel/releases/tag/v1.1.0"))
	m.put("/raw/acme/plugin-rel/v1.1.0/plugin/manifest.json", []byte(demoManifest("rel", "1.1.0")))
	syncNow(t)
	if got := installedFile(t, "rel", "index.js"); got != "rel 1.0" {
		t.Errorf("updated before the package was attached: %q", got)
	}
	m.put("/web/acme/plugin-rel/releases/download/v1.1.0/rel-1.1.0.tar.gz", makeTar(t, []tent{
		{name: "rel/manifest.json", body: demoManifest("rel", "1.1.0")}, {name: "rel/index.js", body: "rel 1.1"},
	}))
	syncNow(t)
	if got := installedFile(t, "rel", "index.js"); got != "rel 1.1" {
		t.Errorf("rel not updated: %q", got)
	}

	// Another organisation: the same plugins are unsigned again.
	_ = os.WriteFile(configmod.Path, []byte("[plugins]\nallow_unsigned = false\ngithub_org = \"other\"\n"), 0o644)
	for _, in := range list(true) {
		if !in.Blocked {
			t.Errorf("%s still trusted under another organisation", in.ID)
		}
	}
	if err := uninstall("tool"); err != nil {
		t.Fatal(err)
	}
	if _, ok := readState().Origins["tool"]; ok {
		t.Error("origin kept after uninstalling")
	}
}

func TestWithoutFiles(t *testing.T) {
	src := []byte(`{"id":"demo","files":{"index.js":""},"version":"1.0.0"}`)
	if got := string(withoutFiles(src)); got != `{"id":"demo","version":"1.0.0"}` {
		t.Errorf("got %s", got)
	}
}
