package update

import (
	"context"
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
