//go:build !windows

package update

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/config"
)

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
