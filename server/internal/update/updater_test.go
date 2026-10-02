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

	"github.com/ervisio/ervisio/server/internal/config"
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

func TestApplyEndToEnd(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.gh.publish("1.1.0", h.sk, nil)
	v, err := h.apply("1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if v != "1.1.0" {
		t.Fatalf("version %q", v)
	}
	l := h.u.Layout
	if b, _ := os.ReadFile(l.DaemonPath("1.1.0")); string(b) != "daemon 1.1.0" {
		t.Fatalf("installed daemon = %q", b)
	}
	if fi, _ := os.Stat(l.DaemonPath("1.1.0")); fi.Mode().Perm() != 0o755 {
		t.Fatalf("daemon mode %v", fi.Mode().Perm())
	}
	mustCurrent(t, l, "1.0.0") // the switch is the helper's job
	if len(h.launched) != 1 || strings.Join(h.launched[0], " ") != l.DaemonPath("1.1.0")+" --apply-update 1.1.0 --kind update" {
		t.Fatalf("launched %v", h.launched)
	}
	var phases []string
	sawFull := false
	for _, e := range h.events {
		if len(phases) == 0 || phases[len(phases)-1] != e.Phase {
			phases = append(phases, e.Phase)
		}
		if e.Phase == "download" && e.Percent == 100 && e.Total > 0 {
			sawFull = true
		}
	}
	if strings.Join(phases, ",") != "check,download,verify,download,verify,extract,test,install,restart" || !sawFull {
		t.Fatalf("phases %v (100%% seen: %v)", phases, sawFull)
	}
	// Staging is cleaned and the lock released for the helper.
	if ents, _ := os.ReadDir(h.u.State.StagingDir()); len(ents) != 0 {
		t.Fatalf("staging left: %v", ents)
	}
	unlock, err := h.u.State.Lock()
	if err != nil {
		t.Fatal("lock still held after handoff")
	}
	unlock()
}

func TestApplyRefusals(t *testing.T) {
	cases := map[string]struct {
		mutate  func(files map[string][]byte)
		want    string
		current string
		before  func(h *harness)
		errIs   error
		errHas  string
	}{
		"tampered archive": {mutate: func(f map[string][]byte) {
			b := f[ArchiveName("1.1.0", "amd64")]
			b[len(b)-10] ^= 0xff
		}, errHas: "does not match its sha256"},
		"tampered sums": {mutate: func(f map[string][]byte) {
			f[SumsFile] = append([]byte{}, f[SumsFile]...)
			f[SumsFile][0] ^= 1
		}, errHas: "signature"},
		"missing signature": {mutate: func(f map[string][]byte) { delete(f, SigFile) }, errHas: "not signed"},
		"foreign key": {mutate: func(f map[string][]byte) {
			_, other := testKey(t)
			f[SigFile] = SignSums(f[SumsFile], other)
		}, errHas: "release key"},
		"no build for arch": {mutate: func(f map[string][]byte) { delete(f, ArchiveName("1.1.0", "amd64")) }, errIs: ErrNoBuild},
		"latest changed":    {want: "1.0.5", errIs: ErrChanged},
		"up to date":        {current: "1.1.0", errIs: ErrUpToDate},
		"newer installed":   {current: "2.0.0", errIs: ErrUpToDate},
		"package busy": {before: func(h *harness) {
			b, _ := json.Marshal(map[string]any{"running": true, "pid": os.Getpid()})
			os.WriteFile(h.u.State.TxFile, b, 0o644)
		}, errIs: ErrPackages},
		"update running": {before: func(h *harness) {
			h.u.State.WriteLast(Result{State: StateRunning, To: "1.1.0", PID: os.Getpid()})
		}, errIs: ErrBusy},
		"binary does not run": {before: func(h *harness) {
			h.u.Probe = func(context.Context, string) (string, error) { return "", errors.New("exec format error") }
		}, errHas: "does not run on this machine"},
		"binary reports other version": {before: func(h *harness) {
			h.u.Probe = func(context.Context, string) (string, error) { return "1.0.9", nil }
		}, errHas: "reports version"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cur := c.current
			if cur == "" {
				cur = "1.0.0"
			}
			h := newHarness(t, cur)
			h.gh.publish("1.1.0", h.sk, c.mutate)
			if c.before != nil {
				c.before(h)
			}
			want := c.want
			if want == "" {
				want = "1.1.0"
			}
			_, err := h.apply(want)
			if err == nil {
				t.Fatal("accepted")
			}
			if c.errIs != nil && !errors.Is(err, c.errIs) {
				t.Fatalf("err = %v, want %v", err, c.errIs)
			}
			if c.errHas != "" && !strings.Contains(err.Error(), c.errHas) {
				t.Fatalf("err = %v, want it to mention %q", err, c.errHas)
			}
			if len(h.launched) != 0 {
				t.Fatal("helper launched after a refusal")
			}
			if _, err := os.Stat(h.u.Layout.VersionDir("1.1.0")); cur != "1.1.0" && !os.IsNotExist(err) {
				t.Fatal("version installed after a refusal")
			}
			ents, _ := os.ReadDir(h.u.Layout.VersionsDir())
			for _, e := range ents {
				if strings.HasPrefix(e.Name(), ".") {
					t.Fatalf("partial folder left: %s", e.Name())
				}
			}
		})
	}
}

func TestApplyMigratesFlatInstall(t *testing.T) {
	h := newHarness(t, "1.0.0")
	l := h.u.Layout
	// Turn the harness layout into a flat install.
	os.RemoveAll(l.VersionsDir())
	os.Remove(filepath.Join(l.LibDir, "current"))
	os.MkdirAll(filepath.Dir(l.BinLink), 0o755)
	os.WriteFile(l.BinLink, []byte("daemon 1.0.0"), 0o755)
	os.WriteFile(filepath.Join(l.LibDir, "ervisio-bridge"), []byte("bridge 1.0.0"), 0o755)
	if l.Kind() != KindFlat {
		t.Fatal("not flat")
	}
	h.gh.publish("1.1.0", h.sk, nil)
	if _, err := h.apply(""); err != nil {
		t.Fatal(err)
	}
	mustCurrent(t, l, "1.0.0")
	if got := strings.Join(l.Installed(), ","); got != "1.0.0,1.1.0" {
		t.Fatalf("installed %s", got)
	}
}

func TestRollbackLaunch(t *testing.T) {
	h := newHarness(t, "1.1.0")
	l := h.u.Layout
	if _, _, err := h.u.Rollback(context.Background(), ""); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("rollback without previous: %v", err)
	}
	addVersion(t, l, "1.0.0")
	l.SetPrevious("1.0.0")
	if _, _, err := h.u.Rollback(context.Background(), "0.9.0"); !errors.Is(err, ErrChanged) {
		t.Fatalf("rollback to another version: %v", err)
	}
	v, _, err := h.u.Rollback(context.Background(), "1.0.0")
	if err != nil || v != "1.0.0" {
		t.Fatal(v, err)
	}
	// The helper is the running (current) version's binary.
	if got := strings.Join(h.launched[0], " "); got != l.DaemonPath("1.1.0")+" --apply-update 1.0.0 --kind rollback" {
		t.Fatalf("launched %s", got)
	}
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

func TestAutoInstallWindow(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.gh.publish("1.1.0", h.sk, nil)
	cfg := config.Default()
	cfg.Updates.AutoInstall = true
	cfg.Updates.AutoInstallAt = "03:30"
	now := time.Date(2026, 10, 2, 2, 0, 0, 0, time.Local)
	var logs []string
	a := &Auto{Updater: h.u, Config: func() *config.Config { return cfg }, Now: func() time.Time { return now },
		Logf: func(f string, args ...any) { logs = append(logs, f) }}
	ctx := context.Background()
	a.Tick(ctx)
	if len(h.launched) != 0 {
		t.Fatal("installed outside the window")
	}
	now = now.Add(90 * time.Minute) // 03:30
	a.Tick(ctx)
	if len(h.launched) != 1 || !strings.Contains(strings.Join(h.launched[0], " "), "--auto") {
		t.Fatalf("launched %v", h.launched)
	}
	now = now.Add(time.Minute)
	a.Tick(ctx)
	if len(h.launched) != 1 {
		t.Fatal("installed twice the same day")
	}

	// A package transaction blocks the automatic install.
	h2 := newHarness(t, "1.0.0")
	h2.gh.publish("1.1.0", h2.sk, nil)
	b, _ := json.Marshal(map[string]any{"running": true, "pid": os.Getpid()})
	os.WriteFile(h2.u.State.TxFile, b, 0o644)
	a2 := &Auto{Updater: h2.u, Config: func() *config.Config { return cfg }, Now: func() time.Time { return now.Add(-time.Minute) }}
	a2.Tick(ctx)
	if len(h2.launched) != 0 {
		t.Fatal("installed during a package transaction")
	}
	if last := h2.u.State.ReadLast(); last == nil || last.State != StateFailed || !last.Auto {
		t.Fatalf("failure not recorded: %+v", last)
	}

	// Off: nothing happens.
	cfg.Updates.AutoInstall = false
	h3 := newHarness(t, "1.0.0")
	h3.gh.publish("1.1.0", h3.sk, nil)
	a3 := &Auto{Updater: h3.u, Config: func() *config.Config { return cfg }, Now: func() time.Time { return now.Add(-time.Minute) }}
	a3.Tick(ctx)
	if len(h3.launched) != 0 {
		t.Fatal("installed with auto_install off")
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
