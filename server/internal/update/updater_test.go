package update

import (
	"archive/tar"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGitHub serves the releases API and the release files.
type fakeGitHub struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	files    map[string][]byte
	releases []Release // [0] is "latest"
	hits     map[string]int
	etag     string
	status   int // forced status for API calls (0 = normal)
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{t: t, files: map[string][]byte{}, hits: map[string]int{}, etag: `"v1"`}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits[r.URL.Path]++
	switch {
	case strings.HasPrefix(r.URL.Path, "/repos/ervisio/ervisio/releases"):
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		if r.Header.Get("If-None-Match") == f.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", f.etag)
		if strings.HasSuffix(r.URL.Path, "/latest") {
			if len(f.releases) == 0 {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(f.releases[0])
			return
		}
		json.NewEncoder(w).Encode(f.releases)
	case strings.HasPrefix(r.URL.Path, "/dl/"):
		b, ok := f.files[strings.TrimPrefix(r.URL.Path, "/dl/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	default:
		http.NotFound(w, r)
	}
}

// publish builds a signed release of version v for amd64.
func (f *fakeGitHub) publish(v string, sk ed25519.PrivateKey, mutate func(files map[string][]byte)) {
	f.t.Helper()
	dir := f.t.TempDir()
	arcName := ArchiveName(v, "amd64")
	pfx := ArchivePrefix(v, "amd64")
	arc := filepath.Join(dir, arcName)
	writeTarGz(f.t, arc, []tarEntry{
		{name: pfx + "/", typ: tar.TypeDir, mode: 0o755},
		{name: pfx + "/bin/ervisiod", body: "daemon " + v, mode: 0o755},
		{name: pfx + "/bin/ervisio-bridge", body: "bridge " + v, mode: 0o755},
		{name: pfx + "/web/index.html", body: "<html>"},
		{name: pfx + "/plugins/docker/manifest.json", body: "{}"},
		{name: pfx + "/VERSION", body: v + "\n"},
	})
	body, _ := os.ReadFile(arc)
	sums := []byte(sumLine(body, arcName))
	files := map[string][]byte{arcName: body, SumsFile: sums, SigFile: SignSums(sums, sk)}
	if mutate != nil {
		mutate(files)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rel := Release{Tag: "v" + v, Name: "Ervisio " + v, Body: "## Changes\n- things", PublishedAt: time.Unix(1700000000, 0).UTC()}
	for name, b := range files {
		f.files[name] = b
		rel.Assets = append(rel.Assets, Asset{Name: name, Size: int64(len(b)), URL: f.srv.URL + "/dl/" + name})
	}
	f.releases = append([]Release{rel}, f.releases...)
	f.etag = `"` + v + `"`
}

type harness struct {
	gh       *fakeGitHub
	u        *Updater
	launched [][]string
	events   []Event
	sk       ed25519.PrivateKey
}

func newHarness(t *testing.T, current string) *harness {
	pk, sk := testKey(t)
	gh := newFakeGitHub(t)
	l, st := setup(t, current)
	l.SetCurrent(current)
	st.TxFile = filepath.Join(t.TempDir(), "tx.json")
	host := strings.TrimPrefix(gh.srv.URL, "https://")
	h := &harness{gh: gh, sk: sk}
	dl := &Downloader{Client: gh.srv.Client(), AllowURL: func(u *url.URL) bool { return u.Scheme == "https" && u.Host == host }}
	h.u = &Updater{
		Layout:   l,
		State:    st,
		Checker:  &Checker{API: gh.srv.URL, Repo: "ervisio/ervisio", Client: gh.srv.Client()},
		Download: dl,
		Keys:     []ed25519.PublicKey{pk},
		Arch:     "amd64",
		Probe: func(_ context.Context, bin string) (string, error) {
			b, err := os.ReadFile(bin)
			if err != nil {
				return "", err
			}
			_, v, _ := strings.Cut(string(b), " ")
			return v, nil
		},
		Launch: func(_ context.Context, unit string, argv []string) error {
			h.launched = append(h.launched, argv)
			return nil
		},
		Current: current,
	}
	return h
}

func (h *harness) apply(want string) (string, error) {
	v, _, err := h.u.Apply(context.Background(), ChannelStable, want, false, func(e Event) { h.events = append(h.events, e) })
	return v, err
}

func TestCheckerCacheAndETag(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.gh.publish("1.1.0", h.sk, nil)
	c := h.u.Checker
	now := time.Unix(1_800_000_000, 0)
	c.Now = func() time.Time { return now }
	ctx := context.Background()
	api := "/repos/ervisio/ervisio/releases/latest"

	rel, _, err := c.Latest(ctx, ChannelStable, false)
	if err != nil || rel.Version() != "1.1.0" {
		t.Fatal(rel, err)
	}
	c.Latest(ctx, ChannelStable, false)
	if h.gh.hits[api] != 1 {
		t.Fatalf("cached answer not used: %d hits", h.gh.hits[api])
	}
	now = now.Add(61 * time.Minute)
	rel, _, err = c.Latest(ctx, ChannelStable, false) // revalidated with ETag -> 304
	if err != nil || rel.Version() != "1.1.0" || h.gh.hits[api] != 2 {
		t.Fatalf("after TTL: %v %v hits=%d", rel, err, h.gh.hits[api])
	}
	h.gh.publish("1.2.0", h.sk, nil) // new ETag
	rel, _, _ = c.Latest(ctx, ChannelStable, false)
	if rel.Version() != "1.1.0" {
		t.Fatal("cache ignored within TTL")
	}
	rel, _, _ = c.Latest(ctx, ChannelStable, true)
	if rel.Version() != "1.2.0" {
		t.Fatalf("force did not refresh: %s", rel.Version())
	}

	// Pre-release channel: highest version among non-drafts.
	h.gh.mu.Lock()
	h.gh.releases = append([]Release{{Tag: "v1.3.0-rc.1", Prerelease: true}, {Tag: "v9.0.0", Draft: true}, {Tag: "nightly"}}, h.gh.releases...)
	h.gh.etag = `"pre"`
	h.gh.mu.Unlock()
	rel, _, err = c.Latest(ctx, ChannelPrerelease, false)
	if err != nil || rel.Version() != "1.3.0-rc.1" {
		t.Fatalf("prerelease: %v %v", rel, err)
	}

	// Errors.
	c2 := &Checker{API: h.gh.srv.URL, Repo: "ervisio/ervisio", Client: h.gh.srv.Client()}
	h.gh.mu.Lock()
	h.gh.status = http.StatusForbidden
	h.gh.mu.Unlock()
	if _, _, err := c2.Latest(ctx, ChannelStable, false); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("403: %v", err)
	}
	h.gh.mu.Lock()
	h.gh.status = 0
	h.gh.releases = nil
	h.gh.etag = `"none"`
	h.gh.mu.Unlock()
	if _, _, err := c2.Latest(ctx, ChannelStable, true); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("no release: %v", err)
	}
}

func TestDownloaderRefusesForeignHostsAndBigFiles(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.gh.publish("1.1.0", h.sk, nil)
	dst := filepath.Join(t.TempDir(), "f")
	if err := h.u.Download.Fetch(context.Background(), "http://example.com/x", dst, 10, nil); err == nil {
		t.Fatal("foreign URL fetched")
	}
	if err := h.u.Download.Fetch(context.Background(), h.gh.srv.URL+"/dl/"+SumsFile, dst, 10, nil); err == nil {
		t.Fatal("file over the limit accepted")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("partial download left")
	}
	for _, raw := range []string{"http://github.com/x", "https://evil.com/x", "https://github.com:8443/x", "https://user@github.com/x", "https://github.com.evil.com/x"} {
		u, _ := url.Parse(raw)
		if DefaultAllowURL(u) {
			t.Errorf("%s allowed", raw)
		}
	}
	for _, raw := range []string{"https://github.com/ervisio/ervisio/releases/download/v1/x", "https://objects.githubusercontent.com/x", "https://release-assets.githubusercontent.com/x"} {
		u, _ := url.Parse(raw)
		if !DefaultAllowURL(u) {
			t.Errorf("%s refused", raw)
		}
	}
}

func TestHTTPHealth(t *testing.T) {
	version := "1.0.0"
	var mu sync.Mutex
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/health":
			json.NewEncoder(w).Encode(HealthInfo{Status: "ok", Version: version})
		case "/api/public/host":
			w.Write([]byte("{}"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	h := &HTTPHealth{Base: srv.URL, Client: srv.Client(), Every: 10 * time.Millisecond}
	ctx := context.Background()
	if err := h.Wait(ctx, "1.0.0", time.Second); err != nil {
		t.Fatal(err)
	}
	if err := h.Wait(ctx, "1.1.0", 100*time.Millisecond); err == nil {
		t.Fatal("wrong version accepted")
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		version = "1.1.0"
		mu.Unlock()
	}()
	if err := h.Wait(ctx, "1.1.0", 2*time.Second); err != nil {
		t.Fatalf("version change not seen: %v", err)
	}
	if err := h.Wait(ctx, "", time.Second); err != nil {
		t.Fatalf("old-build check: %v", err)
	}
}
