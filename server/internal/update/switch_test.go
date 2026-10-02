package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testLayout(t *testing.T) *Layout {
	t.Helper()
	root := t.TempDir()
	l := &Layout{
		LibDir:      filepath.Join(root, "usr/lib/ervisio"),
		BinLink:     filepath.Join(root, "usr/bin/ervisiod"),
		FlatWeb:     filepath.Join(root, "usr/share/ervisio/web"),
		FlatPlugins: filepath.Join(root, "usr/share/ervisio/plugins"),
	}
	os.MkdirAll(l.LibDir, 0o755)
	return l
}

// addVersion creates versions/<v> with fake binaries printing nothing; the
// fake prober reads VERSION instead of running them.
func addVersion(t *testing.T, l *Layout, v string) {
	t.Helper()
	d := l.VersionDir(v)
	os.MkdirAll(filepath.Join(d, "bin"), 0o755)
	os.MkdirAll(filepath.Join(d, "web"), 0o755)
	os.WriteFile(filepath.Join(d, "bin", "ervisiod"), []byte("daemon "+v), 0o755)
	os.WriteFile(filepath.Join(d, "bin", "ervisio-bridge"), []byte("bridge "+v), 0o755)
	os.WriteFile(filepath.Join(d, "web", "index.html"), []byte("<html>"), 0o644)
	os.WriteFile(filepath.Join(d, "VERSION"), []byte(v+"\n"), 0o644)
}

// fakeProbe reports the VERSION file of the binary's version folder;
// folders listed in old behave like builds without --version.
func fakeProbe(old ...string) Prober {
	return func(_ context.Context, bin string) (string, error) {
		vdir := filepath.Dir(filepath.Dir(bin))
		for _, o := range old {
			if filepath.Base(vdir) == o {
				return "", errors.New("flag provided but not defined: -version")
			}
		}
		b, err := os.ReadFile(filepath.Join(vdir, "VERSION"))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
}

type fakeService struct {
	restarts  int
	fail      map[int]error // by restart number (1-based)
	onRestart func()
}

func (f *fakeService) Restart(context.Context) error {
	f.restarts++
	if f.onRestart != nil {
		f.onRestart()
	}
	return f.fail[f.restarts]
}

// fakeHealth answers with the version `current` points to, unless that
// version is in broken.
type fakeHealth struct {
	l      *Layout
	broken map[string]bool
	wants  []string
}

func (f *fakeHealth) Wait(_ context.Context, want string, _ time.Duration) error {
	f.wants = append(f.wants, want)
	cur, err := f.l.Current()
	if err != nil {
		return err
	}
	if f.broken[cur] {
		return fmt.Errorf("version %s does not answer", cur)
	}
	got := f.l.VersionFile(cur)
	if want != "" && got != want {
		return fmt.Errorf("version %q answers, expected %q", got, want)
	}
	return nil
}

func setup(t *testing.T, versions ...string) (*Layout, *State) {
	l := testLayout(t)
	for _, v := range versions {
		addVersion(t, l, v)
	}
	st := &State{Dir: filepath.Join(t.TempDir(), "updates"), Owner: os.Getuid(), TxOwner: os.Getuid()}
	return l, st
}

func mustCurrent(t *testing.T, l *Layout, want string) {
	t.Helper()
	cur, err := l.Current()
	if err != nil || cur != want {
		t.Fatalf("current = %q (%v), want %q", cur, err, want)
	}
}

func TestSwitchSuccessKeepsTwo(t *testing.T) {
	l, st := setup(t, "0.8.0", "0.9.0", "1.0.0", "1.1.0")
	l.SetCurrent("1.0.0")
	l.SetPrevious("0.9.0")
	svc := &fakeService{}
	h := &fakeHealth{l: l}
	sw := &Switcher{Layout: l, State: st, Service: svc, Health: h, Probe: fakeProbe()}

	r := sw.Switch(context.Background(), "1.1.0", KindUpdate, false)
	if r.State != StateOK || r.From != "1.0.0" || r.To != "1.1.0" {
		t.Fatalf("result %+v", r)
	}
	mustCurrent(t, l, "1.1.0")
	if p := l.Previous(); p != "1.0.0" {
		t.Fatalf("previous = %q", p)
	}
	if got := l.Installed(); strings.Join(got, ",") != "1.0.0,1.1.0" {
		t.Fatalf("installed after prune = %v (keep current + previous only)", got)
	}
	if svc.restarts != 1 || len(h.wants) != 1 || h.wants[0] != "1.1.0" {
		t.Fatalf("restarts=%d wants=%v", svc.restarts, h.wants)
	}
	if last := st.ReadLast(); last == nil || last.State != StateOK || last.Kind != KindUpdate {
		t.Fatalf("last.json = %+v", last)
	}
	// The link is relative, so the tree can be inspected from anywhere.
	if tgt, _ := os.Readlink(filepath.Join(l.LibDir, "current")); tgt != "versions/1.1.0" {
		t.Fatalf("current -> %q", tgt)
	}
}

func TestSwitchUnhealthyRollsBack(t *testing.T) {
	l, st := setup(t, "0.9.0", "1.0.0", "1.1.0")
	l.SetCurrent("1.0.0")
	l.SetPrevious("0.9.0")
	svc := &fakeService{}
	h := &fakeHealth{l: l, broken: map[string]bool{"1.1.0": true}}
	sw := &Switcher{Layout: l, State: st, Service: svc, Health: h, Probe: fakeProbe()}

	r := sw.Switch(context.Background(), "1.1.0", KindUpdate, false)
	if r.State != StateRolledBack || !strings.Contains(r.Error, "does not answer") {
		t.Fatalf("result %+v", r)
	}
	mustCurrent(t, l, "1.0.0")
	if p := l.Previous(); p != "0.9.0" {
		t.Fatalf("previous changed to %q", p)
	}
	if _, err := os.Stat(l.VersionDir("1.1.0")); !os.IsNotExist(err) {
		t.Fatal("broken version kept")
	}
	if svc.restarts != 2 || strings.Join(h.wants, ",") != "1.1.0,1.0.0" {
		t.Fatalf("restarts=%d wants=%v", svc.restarts, h.wants)
	}
	last := st.ReadLast()
	if last == nil || last.State != StateRolledBack || last.From != "1.0.0" || last.To != "1.1.0" {
		t.Fatalf("last.json = %+v", last)
	}
}

func TestSwitchRestartFailureRollsBack(t *testing.T) {
	l, st := setup(t, "1.0.0", "1.1.0")
	l.SetCurrent("1.0.0")
	var seen []string
	svc := &fakeService{fail: map[int]error{1: errors.New("unit failed")}}
	svc.onRestart = func() { c, _ := l.Current(); seen = append(seen, c) }
	sw := &Switcher{Layout: l, State: st, Service: svc, Health: &fakeHealth{l: l}, Probe: fakeProbe()}
	r := sw.Switch(context.Background(), "1.1.0", KindUpdate, false)
	if r.State != StateRolledBack || !strings.Contains(r.Error, "unit failed") {
		t.Fatalf("result %+v", r)
	}
	mustCurrent(t, l, "1.0.0")
	if strings.Join(seen, ",") != "1.1.0,1.0.0" {
		t.Fatalf("restarted with current = %v", seen)
	}
}

func TestRollbackKindSwapsVersions(t *testing.T) {
	l, st := setup(t, "1.0.0", "1.1.0")
	l.SetCurrent("1.1.0")
	l.SetPrevious("1.0.0")
	sw := &Switcher{Layout: l, State: st, Service: &fakeService{}, Health: &fakeHealth{l: l}, Probe: fakeProbe()}
	r := sw.Switch(context.Background(), "1.0.0", KindRollback, false)
	if r.State != StateOK || r.Kind != KindRollback {
		t.Fatalf("result %+v", r)
	}
	mustCurrent(t, l, "1.0.0")
	if p := l.Previous(); p != "1.1.0" {
		t.Fatalf("previous = %q, want 1.1.0 (roll forward possible)", p)
	}
	if strings.Join(l.Installed(), ",") != "1.0.0,1.1.0" {
		t.Fatalf("installed = %v", l.Installed())
	}
}

func TestSwitchToOldBuildWithoutVersionFlag(t *testing.T) {
	l, st := setup(t, "legacy", "1.1.0")
	l.SetCurrent("1.1.0")
	l.SetPrevious("legacy")
	h := &fakeHealth{l: l}
	sw := &Switcher{Layout: l, State: st, Service: &fakeService{}, Health: h, Probe: fakeProbe("legacy")}
	r := sw.Switch(context.Background(), "legacy", KindRollback, false)
	if r.State != StateOK {
		t.Fatalf("result %+v", r)
	}
	if len(h.wants) != 1 || h.wants[0] != "" {
		t.Fatalf("health wanted %q, expected any version for an old build", h.wants)
	}
}

func TestSwitchRefusesMissingOrSame(t *testing.T) {
	l, st := setup(t, "1.0.0")
	l.SetCurrent("1.0.0")
	svc := &fakeService{}
	sw := &Switcher{Layout: l, State: st, Service: svc, Health: &fakeHealth{l: l}, Probe: fakeProbe()}
	for _, target := range []string{"1.0.0", "2.0.0", "../../etc", ""} {
		r := sw.Switch(context.Background(), target, KindUpdate, false)
		if r.State != StateFailed {
			t.Fatalf("%q: result %+v", target, r)
		}
	}
	mustCurrent(t, l, "1.0.0")
	if svc.restarts != 0 {
		t.Fatal("restarted although nothing was switched")
	}
}

func TestMigrateFlat(t *testing.T) {
	l := testLayout(t)
	os.MkdirAll(filepath.Dir(l.BinLink), 0o755)
	os.WriteFile(l.BinLink, []byte("old daemon"), 0o755)
	os.WriteFile(filepath.Join(l.LibDir, "ervisio-bridge"), []byte("old bridge"), 0o755)
	os.MkdirAll(filepath.Join(l.FlatWeb, "assets"), 0o755)
	os.WriteFile(filepath.Join(l.FlatWeb, "index.html"), []byte("<old>"), 0o644)
	os.MkdirAll(filepath.Join(l.FlatPlugins, "docker"), 0o755)
	os.WriteFile(filepath.Join(l.FlatPlugins, "docker", "manifest.json"), []byte("{}"), 0o644)
	if k := l.Kind(); k != KindFlat {
		t.Fatalf("kind = %s", k)
	}
	name, err := l.Migrate("0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if name != "0.1.0" || l.Kind() != KindVersioned {
		t.Fatalf("name=%q kind=%s", name, l.Kind())
	}
	mustCurrent(t, l, "0.1.0")
	for link, want := range map[string]string{
		l.BinLink: "old daemon",
		filepath.Join(l.LibDir, "ervisio-bridge"):                            "old bridge",
		filepath.Join(l.VersionDir("0.1.0"), "web", "index.html"):            "<old>",
		filepath.Join(l.VersionDir("0.1.0"), "plugins/docker/manifest.json"): "{}",
	} {
		b, err := os.ReadFile(link)
		if err != nil || string(b) != want {
			t.Errorf("%s = %q, %v", link, b, err)
		}
	}
	if fi, _ := os.Lstat(l.BinLink); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("BinLink is not a symlink after migration")
	}
	if tgt, _ := os.Readlink(l.BinLink); tgt != filepath.Join(l.LibDir, "current/bin/ervisiod") {
		t.Fatalf("BinLink -> %s", tgt)
	}
	// The flat web folder is kept for an old binary that would be rolled back to.
	if _, err := os.Stat(filepath.Join(l.FlatWeb, "index.html")); err != nil {
		t.Fatal("flat web folder removed")
	}
	// Migrating again is a no-op.
	if n, err := l.Migrate("x"); err != nil || n != "0.1.0" {
		t.Fatalf("second migrate: %q %v", n, err)
	}
	// Following an update, the entry points follow `current`.
	addVersion(t, l, "0.2.0")
	l.SetCurrent("0.2.0")
	if b, _ := os.ReadFile(l.BinLink); string(b) != "daemon 0.2.0" {
		t.Fatalf("BinLink resolves to %q", b)
	}
}

func TestLayoutKindNone(t *testing.T) {
	l := testLayout(t)
	if l.Kind() != KindNone {
		t.Fatal("empty layout not none")
	}
	if _, err := l.Migrate("1.0.0"); err == nil {
		t.Fatal("migrated nothing")
	}
	// A current link pointing outside versions/ is not trusted.
	os.Symlink("/etc", filepath.Join(l.LibDir, "current"))
	if _, err := l.Current(); err == nil {
		t.Fatal("current -> /etc accepted")
	}
}

func TestVersionDirOf(t *testing.T) {
	if d, ok := versionDirOf("/usr/lib/ervisio/versions/1.2.0/bin/ervisiod", "/usr/lib/ervisio"); !ok || d != "/usr/lib/ervisio/versions/1.2.0" {
		t.Fatalf("got %q %v", d, ok)
	}
	for _, exe := range []string{"/usr/bin/ervisiod", "/home/u/Ervisio/server/bin/ervisiod", "/usr/lib/ervisio/ervisio-bridge", "/usr/lib/ervisio/versions/.x/bin/ervisiod"} {
		if _, ok := versionDirOf(exe, "/usr/lib/ervisio"); ok {
			t.Errorf("%s detected as versioned", exe)
		}
	}
}

func TestStateFiles(t *testing.T) {
	_, st := setup(t)
	if st.ReadLast() != nil || st.Running() {
		t.Fatal("empty state")
	}
	st.WriteLast(Result{State: StateRunning, Kind: KindUpdate, To: "1.1.0", PID: os.Getpid()})
	if !st.Running() {
		t.Fatal("running update not seen")
	}
	st.WriteLast(Result{State: StateRunning, Kind: KindUpdate, To: "1.1.0", PID: 1 << 30})
	if r := st.ReadLast(); r.State != StateFailed || r.Error != "interrupted" {
		t.Fatalf("dead helper reported as %+v", r)
	}
	if fi, _ := os.Stat(filepath.Join(st.Dir, "last.json")); fi.Mode().Perm() != 0o644 {
		t.Fatalf("last.json mode %v", fi.Mode().Perm())
	}
	// Owned by someone else: ignored.
	other := *st
	other.Owner = os.Getuid() + 1
	if other.ReadLast() != nil {
		t.Fatal("last.json of another owner trusted")
	}

	// Package transaction state.
	st.TxFile = filepath.Join(t.TempDir(), "tx.json")
	if st.PackageTransactionRunning() {
		t.Fatal("no file means no transaction")
	}
	b, _ := json.Marshal(map[string]any{"running": true, "pid": os.Getpid()})
	os.WriteFile(st.TxFile, b, 0o644)
	if !st.PackageTransactionRunning() {
		t.Fatal("running transaction not seen")
	}
	b, _ = json.Marshal(map[string]any{"running": true, "pid": 1 << 30})
	os.WriteFile(st.TxFile, b, 0o644)
	if st.PackageTransactionRunning() {
		t.Fatal("dead transaction reported as running")
	}

	// Staging folder is private.
	if err := st.Prepare(); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(st.StagingDir()); fi.Mode().Perm() != 0o700 {
		t.Fatalf("staging mode %v", fi.Mode().Perm())
	}
	// Lock is exclusive.
	unlock, err := st.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Lock(); err == nil {
		t.Fatal("second lock granted")
	}
	unlock()
	u2, err := st.Lock()
	if err != nil {
		t.Fatal("lock not released")
	}
	u2()
}

func TestLoopbackAddr(t *testing.T) {
	for in, want := range map[string]string{
		"0.0.0.0:9090":  "127.0.0.1:9090",
		":9443":         "127.0.0.1:9443",
		"[::]:9090":     "[::1]:9090",
		"10.0.0.5:9090": "10.0.0.5:9090",
		"garbage":       "127.0.0.1:9090",
	} {
		if got := LoopbackAddr(in); got != want {
			t.Errorf("LoopbackAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
