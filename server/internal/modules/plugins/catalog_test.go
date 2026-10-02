package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
)

// testKey is a fixed throwaway key (never the team key).
func testKey(seed byte) (ed25519.PublicKey, ed25519.PrivateKey) {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = seed
	}
	k := ed25519.NewKeyFromSeed(s)
	return k.Public().(ed25519.PublicKey), k
}

// A fixed vector, shared with the registry's tools (Ervisio/plugins): the
// signature covers "ervisio-catalog-v1\n" + the canonical JSON, so
// formatting does not matter but content does.
func TestCatalogSignature(t *testing.T) {
	pub, priv := testKey(7)
	cat := []byte(`{"plugins": [{"id":"a&b","n":1e3,"x":"<é>"}], "categories": []}`)
	sig, err := SignCatalog(cat, priv)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := CatalogMessage(cat)
	if string(msg) != "ervisio-catalog-v1\n"+`{"categories":[],"plugins":[{"id":"a&b","n":1e3,"x":"<é>"}]}` {
		t.Fatalf("message %q", msg)
	}
	if err := VerifyCatalog([]byte("{ \"categories\":[],\n\"plugins\":[{\"x\":\"<é>\",\"n\":1e3,\"id\":\"a&b\"}]}"), sig, []ed25519.PublicKey{pub}); err != nil {
		t.Fatalf("reformatted catalog: %v", err)
	}
	if err := VerifyCatalog([]byte(`{"categories":[],"plugins":[]}`), sig, []ed25519.PublicKey{pub}); err == nil {
		t.Fatal("changed catalog verified")
	}
	other, _ := testKey(8)
	if err := VerifyCatalog(cat, sig, []ed25519.PublicKey{other}); err == nil {
		t.Fatal("wrong key verified")
	}
	if err := VerifyCatalog(cat, []byte("bm90IGEgc2lnbmF0dXJl\n"), []ed25519.PublicKey{pub}); err == nil {
		t.Fatal("garbage signature verified")
	}
	// A plugin signature over the same bytes is not a catalog signature.
	pm, _ := SigningMessage(cat)
	if err := VerifyCatalog(cat, []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, pm))), []ed25519.PublicKey{pub}); err == nil {
		t.Fatal("a plugin signature was accepted as a catalog signature")
	}
}

func TestCatalogSigURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://ervisio.github.io/plugins/catalog.json": "https://ervisio.github.io/plugins/catalog.sig",
		"https://example.org/c.json?x=1":                 "https://example.org/c.sig",
		"https://example.org/catalog":                    "https://example.org/catalog.sig",
	} {
		u, _ := url.Parse(in)
		if got := catalogSigURL(u); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

// market is a fake https registry: catalog.json, catalog.sig and packages.
type market struct {
	srv   *httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	hits  map[string]int
}

func newMarket(t *testing.T) *market {
	m := &market{files: map[string][]byte{}, hits: map[string]int{}}
	m.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.hits[r.URL.Path]++
		b, ok := m.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(m.srv.Close)
	oldC, oldD := catalogClient, downloadClient
	catalogClient = m.srv.Client
	downloadClient = m.srv.Client
	t.Cleanup(func() {
		catalogClient, downloadClient = oldC, oldD
		remoteMu.Lock()
		remoteLast = nil
		remoteMu.Unlock()
	})
	remoteMu.Lock()
	remoteLast = nil
	remoteMu.Unlock()
	return m
}

func (m *market) put(path string, b []byte) {
	m.mu.Lock()
	m.files[path] = b
	m.mu.Unlock()
}

func (m *market) url(path string) string { return m.srv.URL + path }

// publish signs catalog with priv and serves it at /catalog.json.
func (m *market) publish(t *testing.T, c *Catalog, priv ed25519.PrivateKey) {
	t.Helper()
	b, _ := json.Marshal(c)
	m.put("/catalog.json", b)
	if priv != nil {
		sig, err := SignCatalog(b, priv)
		if err != nil {
			t.Fatal(err)
		}
		m.put("/catalog.sig", sig)
	}
}

// useRemote points the configuration at the fake market.
func useRemote(t *testing.T, m *market, extra string) {
	t.Helper()
	noRemoteCatalog = false
	conf := "[plugins]\nallow_unsigned = false\ncatalog_url = \"" + m.url("/catalog.json") + "\"\n" + extra
	if err := os.WriteFile(configmod.Path, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	remoteMu.Lock()
	remoteLast = nil
	remoteMu.Unlock()
}

func TestRemoteCatalogSigned(t *testing.T) {
	setup(t)
	pub, priv := testKey(1)
	oldKeys := Keys
	Keys = func() []ed25519.PublicKey { return []ed25519.PublicKey{pub} }
	t.Cleanup(func() { Keys = oldKeys })
	m := newMarket(t)
	cat := &Catalog{Categories: []CatalogCategory{{ID: "x", Name: "X"}}, Plugins: []CatalogEntry{{ID: "remote", Name: "Remote", Version: "1.0.0", Verified: true}}}

	// Signed by the team key: offered.
	m.publish(t, cat, priv)
	useRemote(t, m, "")
	v, err := catalogView(context.Background())
	if err != nil || v.Warning != "" || len(v.Plugins) != 1 || v.Plugins[0].ID != "remote" || len(v.Categories) != 1 {
		t.Fatalf("%+v %v", v, err)
	}
	// Cached: no second fetch within 5 minutes.
	catalogView(context.Background())
	if m.hits["/catalog.json"] != 1 {
		t.Errorf("fetched %d times", m.hits["/catalog.json"])
	}

	// Signed by another key: ignored, with a warning.
	_, otherPriv := testKey(2)
	m.publish(t, cat, otherPriv)
	useRemote(t, m, "")
	v, _ = catalogView(context.Background())
	if len(v.Plugins) != 0 || !strings.Contains(v.Warning, "ignored") {
		t.Fatalf("catalog with a foreign signature: %+v", v)
	}
	// Unless plugins.catalog_key trusts that key.
	otherPub, _ := testKey(2)
	useRemote(t, m, "catalog_key = \""+base64.StdEncoding.EncodeToString(otherPub)+"\"\n")
	v, _ = catalogView(context.Background())
	if len(v.Plugins) != 1 || v.Warning != "" {
		t.Fatalf("catalog_key not trusted: %+v", v)
	}

	// Unsigned: ignored.
	delete(m.files, "/catalog.sig")
	useRemote(t, m, "")
	v, _ = catalogView(context.Background())
	if len(v.Plugins) != 0 || !strings.Contains(v.Warning, "signature") {
		t.Fatalf("unsigned catalog: %+v", v)
	}

	// Tampered after signing: ignored.
	m.publish(t, cat, priv)
	b := m.files["/catalog.json"]
	m.put("/catalog.json", []byte(strings.Replace(string(b), "Remote", "Evil", 1)))
	useRemote(t, m, "")
	v, _ = catalogView(context.Background())
	if len(v.Plugins) != 0 || v.Warning == "" {
		t.Fatalf("tampered catalog: %+v", v)
	}

	// catalog_url = "" turns the remote catalog off; the override file still works.
	os.WriteFile(configmod.Path, []byte("[plugins]\ncatalog_url = \"\"\n"), 0o644)
	remoteLast = nil
	v, _ = catalogView(context.Background())
	if len(v.Plugins) != 0 || v.Warning != "" {
		t.Fatalf("disabled: %+v", v)
	}
	m.publish(t, cat, priv)
	os.WriteFile(CatalogURLFile, []byte(m.url("/catalog.json")+"\n"), 0o644)
	remoteLast = nil
	v, _ = catalogView(context.Background())
	if len(v.Plugins) != 1 {
		t.Fatalf("override file: %+v", v)
	}
	os.Remove(CatalogURLFile)
}

// signedPackage builds a signed .tar.gz of a demo plugin with id.
func signedPackage(t *testing.T, id, version string, priv ed25519.PrivateKey) ([]byte, *Manifest) {
	t.Helper()
	src := filepath.Join(t.TempDir(), id)
	man := strings.NewReplacer(`"id": "demo"`, `"id": "`+id+`"`, `"version": "1.2.3"`, `"version": "`+version+`"`,
		`"visibleTo": {"groups": ["docker"]}`, `"visibleTo": {"groups": []}`).Replace(goodManifest)
	writePlugin(t, src, man, map[string]string{"index.js": "export default () => {}"})
	if err := SignFolder(src, priv); err != nil {
		t.Fatal(err)
	}
	var ents []tent
	ents = append(ents, tent{name: id + "/", typ: '5', mode: 0o755})
	for _, n := range []string{"index.js", "manifest.json", "manifest.sig"} {
		b, _ := os.ReadFile(filepath.Join(src, n))
		ents = append(ents, tent{name: id + "/" + n, body: string(b)})
	}
	m, err := LoadManifest(src)
	if err != nil {
		t.Fatal(err)
	}
	return makeTar(t, ents), m
}

func entryFor(m *Manifest, source string, pkg []byte) CatalogEntry {
	sum := sha256.Sum256(pkg)
	return CatalogEntry{ID: m.ID, Name: m.Name, Version: m.Version, Verified: true, Source: source, SHA256: hex.EncodeToString(sum[:]),
		Capabilities: m.Capabilities, Contributes: m.Contributes, VisibleTo: m.VisibleTo}
}

// fakeDocker makes a unix socket stand for the Docker engine.
func fakeDocker(t *testing.T, present bool) {
	t.Helper()
	old := DockerSockets
	p := filepath.Join(t.TempDir(), "docker.sock")
	DockerSockets = []string{p}
	t.Cleanup(func() { DockerSockets = old })
	if present {
		l, err := net.Listen("unix", p)
		if err != nil {
			t.Skip("unix sockets unavailable:", err)
		}
		t.Cleanup(func() { l.Close() })
	}
}

func TestMovedDockerInstalledFromMarketplace(t *testing.T) {
	_, installed := setup(t)
	pub, priv := testKey(3)
	oldKeys := Keys
	Keys = func() []ed25519.PublicKey { return []ed25519.PublicKey{pub} }
	t.Cleanup(func() { Keys = oldKeys })
	m := newMarket(t)
	pkg, man := signedPackage(t, "docker", "2.0.1", priv)
	m.put("/docker-2.0.1.tar.gz", pkg)
	m.publish(t, &Catalog{Plugins: []CatalogEntry{entryFor(man, m.url("/docker-2.0.1.tar.gz"), pkg)}}, priv)
	useRemote(t, m, "")
	logf := func(f string, a ...any) { t.Logf(f, a...) }

	// No Docker on the host: nothing installed, not retried.
	fakeDocker(t, false)
	if migrateMovedOnce(context.Background(), logf) || ReadMoved()["docker"] != MovedNotNeeded {
		t.Fatalf("no docker: %v", ReadMoved())
	}
	if v, _ := catalogView(context.Background()); len(v.Moved) != 0 {
		t.Fatalf("card without docker: %+v", v.Moved)
	}

	// Docker present (a machine updated from 0.3.0): installed once, from the signed catalog.
	os.Remove(MovedStatePath)
	fakeDocker(t, true)
	if v, _ := catalogView(context.Background()); len(v.Moved) != 1 || v.Moved[0].ID != "docker" {
		t.Fatalf("card expected before the install: %+v", v.Moved)
	}
	if migrateMovedOnce(context.Background(), logf) {
		t.Fatal("still pending")
	}
	if ReadMoved()["docker"] != MovedInstalled {
		t.Fatalf("%v", ReadMoved())
	}
	s := CheckSignature(filepath.Join(installed, "docker"), Keys())
	if !s.Verified {
		t.Fatalf("installed copy: %+v", s)
	}
	if v, _ := catalogView(context.Background()); len(v.Moved) != 0 {
		t.Fatalf("card after the install: %+v", v.Moved)
	}
	// Removed by the admin afterwards: never reinstalled.
	if err := uninstall("docker"); err != nil {
		t.Fatal(err)
	}
	migrateMovedOnce(context.Background(), logf)
	if _, err := os.Stat(filepath.Join(installed, "docker")); err == nil {
		t.Fatal("reinstalled after an uninstall")
	}

	// Switched off before the update: skipped, no card.
	os.Remove(MovedStatePath)
	st := readState()
	st.Enabled["docker"] = false
	writeState(st)
	migrateMovedOnce(context.Background(), logf)
	if ReadMoved()["docker"] != MovedSkipped {
		t.Fatalf("%v", ReadMoved())
	}
	if v, _ := catalogView(context.Background()); len(v.Moved) != 0 {
		t.Fatalf("card for a disabled plugin: %+v", v.Moved)
	}
	delete(st.Enabled, "docker")
	writeState(st)

	// The catalog is unreachable or unsigned: pending, retried, card shown.
	os.Remove(MovedStatePath)
	delete(m.files, "/catalog.sig")
	useRemote(t, m, "")
	if !migrateMovedOnce(context.Background(), logf) || ReadMoved()["docker"] != MovedPending {
		t.Fatalf("expected pending: %v", ReadMoved())
	}
	// A package whose signature does not verify is never installed.
	evilPkg, evilMan := signedPackage(t, "docker", "2.0.1", func() ed25519.PrivateKey { _, k := testKey(9); return k }())
	m.put("/docker-2.0.1.tar.gz", evilPkg)
	m.publish(t, &Catalog{Plugins: []CatalogEntry{entryFor(evilMan, m.url("/docker-2.0.1.tar.gz"), evilPkg)}}, priv)
	useRemote(t, m, "")
	if !migrateMovedOnce(context.Background(), logf) {
		t.Fatal("a package signed by another key was installed")
	}
	if _, err := os.Stat(filepath.Join(installed, "docker")); err == nil {
		t.Fatal("installed a package with a foreign signature")
	}
}
