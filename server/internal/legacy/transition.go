package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/update"
)

// Transition moves a running LinuxAdmin to Ervisio version Version, whose
// files LinuxAdmin's updater (or installer) put in
// /usr/lib/linuxadmin/versions/<Version>. It runs outside both services (a
// transient systemd unit):
//
//  1. installs that folder as /usr/lib/ervisio/versions/<Version> (binaries
//     renamed), `current` and /usr/bin/ervisiod;
//  2. imports the data (Import with Mark) and writes ervisio.service;
//  3. stops linuxadmin.service, starts ervisio.service and waits until it
//     answers as Version;
//  4. on success enables ervisio.service instead of linuxadmin.service
//     (when that was enabled), finalizes the import, moves the scheduled
//     update; on failure stops ervisio.service, starts linuxadmin.service
//     again and removes everything step 1 and 2 created.
//
// LinuxAdmin's own files are not changed, apart from last.json in its
// update state (so its web app shows the result) and the note in
// /etc/linuxadmin. Every step can be repeated: an interrupted transition
// (power cut) leaves linuxadmin.service enabled, and the next attempt
// starts over.
type Transition struct {
	Paths   Paths
	Version string
	Auto    bool
	Systemd Systemd
	// Health waits until ervisio.service answers as version want.
	Health func(ctx context.Context, want string) error
	// LegacyHealth waits until linuxadmin.service answers again (rollback).
	LegacyHealth func(ctx context.Context) error
	Logf         func(format string, args ...any)
}

func (t *Transition) logf(format string, args ...any) {
	if t.Logf != nil {
		t.Logf(format, args...)
	}
}

func (t *Transition) newLayout() *update.Layout {
	p := t.Paths
	return &update.Layout{LibDir: p.At(brand.LibDir), BinLink: p.At(brand.BinLink),
		FlatWeb: p.At(brand.WebDir), FlatPlugins: p.At(brand.PackagedPluginsDir), Managed: p.At(brand.ManagedMarker)}
}

func (t *Transition) legacyLayout() *update.Layout {
	p := t.Paths
	return &update.Layout{LibDir: p.At(brand.LegacyLibDir), BinLink: p.At(brand.LegacyBinLink),
		Managed: p.At(brand.LegacyLibDir + "/managed")}
}

func (t *Transition) legacyState() *update.State {
	return &update.State{Dir: t.Paths.At(brand.LegacyStateDir + "/updates"), Owner: os.Geteuid()}
}

func (t *Transition) newState() *update.State {
	return &update.State{Dir: t.Paths.At(brand.UpdatesDir), Owner: os.Geteuid()}
}

// undo records what to take back on failure.
type undo struct {
	libCreated  bool   // LibDir did not exist
	prevCurrent string // `current` of an existing LibDir ("" none)
	versionNew  bool   // versions/<v> was installed by this run
	binLinkNew  bool
	bridgeLink  bool
	unitNew     bool
	imported    *Result
}

// Run performs the transition and returns its result (also written to
// last.json in both update state folders).
func (t *Transition) Run(ctx context.Context) update.Result {
	v := t.Version
	lst := t.legacyState()
	from, _ := t.legacyLayout().Current()
	r := update.Result{State: update.StateRunning, Kind: update.KindUpdate, From: from, To: v,
		StartedAt: time.Now().UnixMilli(), Auto: t.Auto, PID: os.Getpid()}
	if err := lst.WriteLast(r); err != nil {
		t.logf("write %s/last.json: %v", lst.Dir, err)
	}
	finish := func(state string, err error) update.Result {
		r.State, r.FinishedAt, r.PID = state, time.Now().UnixMilli(), 0
		if err != nil {
			r.Error = err.Error()
			t.logf("move to %s %s: %s: %v", brand.Name, v, state, err)
		}
		for _, st := range []*update.State{lst, t.newState()} {
			if state != update.StateOK && st != lst {
				continue // the Ervisio state folder was taken back
			}
			if werr := st.WriteLast(r); werr != nil {
				t.logf("write %s/last.json: %v", st.Dir, werr)
			}
		}
		if state != update.StateOK {
			writeFailure(t.Paths, v, r.Error)
		} else {
			os.Remove(failurePath(t.Paths))
		}
		return r
	}

	src := t.Paths.At(brand.LegacyLibDir + "/versions/" + v)
	if !update.ValidDirName(v) || !isDir(src) {
		return finish(update.StateFailed, fmt.Errorf("%s is not installed in %s", v, t.Paths.At(brand.LegacyLibDir+"/versions")))
	}
	nl := t.newLayout()
	if by := nl.ManagedBy(); by != "" {
		return finish(update.StateFailed, fmt.Errorf("%s is installed here by %s already; remove %s with its package manager or %s --remove-legacy", brand.Name, by, brand.LegacyName, brand.DaemonBinary))
	}
	if t.Systemd.IsActive(ctx, brand.ServiceUnit) {
		return finish(update.StateFailed, fmt.Errorf("%s is already running here; remove %s with %s --remove-legacy", brand.ServiceUnit, brand.LegacyName, brand.DaemonBinary))
	}

	u := &undo{}
	t.logf("moving %s %s to %s %s", brand.LegacyName, from, brand.Name, v)
	if err := t.installLayout(src, u); err != nil {
		t.rollback(ctx, u, false)
		return finish(update.StateFailed, err)
	}
	im := &Import{Paths: t.Paths, Mark: true, Logf: t.Logf}
	res, err := im.Run()
	u.imported = res
	if err != nil {
		t.rollback(ctx, u, false)
		return finish(update.StateFailed, fmt.Errorf("copy the data: %w", err))
	}
	if u.unitNew, err = InstallUnit(t.Paths, src); err != nil {
		t.rollback(ctx, u, false)
		return finish(update.StateFailed, err)
	}
	if err := t.Systemd.Systemctl(ctx, "daemon-reload"); err != nil {
		t.rollback(ctx, u, false)
		return finish(update.StateFailed, err)
	}

	enabled := t.Systemd.IsEnabled(ctx, brand.LegacyServiceUnit)
	t.logf("stopping %s, starting %s", brand.LegacyServiceUnit, brand.ServiceUnit)
	if err := t.Systemd.Systemctl(ctx, "stop", brand.LegacyServiceUnit); err != nil {
		t.logf("%v", err) // ervisio.service conflicts with it anyway
	}
	err = t.Systemd.Systemctl(ctx, "start", brand.ServiceUnit)
	if err == nil {
		err = t.Health(ctx, v)
	}
	if err != nil {
		t.rollback(ctx, u, true)
		// Like LinuxAdmin's own rollback, drop the version that failed (unless
		// LinuxAdmin's layout runs it: an install made by its install.sh).
		if cur, _ := t.legacyLayout().Current(); cur != v {
			os.RemoveAll(src)
		}
		return finish(update.StateRolledBack, fmt.Errorf("%s %s did not start (%v); %s is running again", brand.Name, v, err, brand.LegacyName))
	}

	// Ervisio answers: make it permanent.
	if enabled {
		if err := t.Systemd.Systemctl(ctx, "enable", brand.ServiceUnit); err != nil {
			t.logf("%v", err)
		}
		if err := t.Systemd.Systemctl(ctx, "disable", brand.LegacyServiceUnit); err != nil {
			t.logf("%v", err)
		}
	}
	if err := Finalize(t.Paths); err != nil {
		t.logf("finalize the copied data: %v", err)
	}
	if err := MigrateSchedule(ctx, t.Paths, t.Systemd, t.Logf); err != nil {
		t.logf("scheduled update: %v", err)
	}
	t.logf("%s %s runs as %s; %s's files are kept for a rollback (see %s/%s)", brand.Name, v, brand.ServiceUnit, brand.LegacyName, brand.LegacyConfigDir, NoteFile)
	return finish(update.StateOK, nil)
}

// installLayout copies the release folder to the Ervisio versioned layout.
func (t *Transition) installLayout(src string, u *undo) error {
	v := t.Version
	nl := t.newLayout()
	u.libCreated = !exists(nl.LibDir)
	u.prevCurrent, _ = nl.Current()
	u.binLinkNew = !exists(nl.BinLink)
	u.bridgeLink = !exists(filepath.Join(nl.LibDir, brand.BridgeBinary))
	if u.prevCurrent != v || !isRegular(nl.DaemonPath(v)) {
		partial, err := nl.NewPartialDir(v)
		if err != nil {
			return err
		}
		if err := copyTree(src, partial, nil); err != nil {
			os.RemoveAll(partial)
			return fmt.Errorf("copy %s: %w", src, err)
		}
		for old, nw := range map[string]string{brand.LegacyDaemonBinary: brand.DaemonBinary, brand.LegacyBridgeBinary: brand.BridgeBinary} {
			o, n := filepath.Join(partial, "bin", old), filepath.Join(partial, "bin", nw)
			if isRegular(o) && !exists(n) {
				if err := os.Rename(o, n); err != nil {
					os.RemoveAll(partial)
					return err
				}
			}
		}
		for _, b := range []string{brand.DaemonBinary, brand.BridgeBinary} {
			if !isRegular(filepath.Join(partial, "bin", b)) {
				os.RemoveAll(partial)
				return fmt.Errorf("%s has no bin/%s", src, b)
			}
		}
		u.versionNew = !exists(nl.VersionDir(v))
		if u.prevCurrent == v {
			// A damaged copy of the active version: replace it in place.
			os.RemoveAll(nl.VersionDir(v))
		}
		if err := nl.InstallFrom(partial, v); err != nil {
			os.RemoveAll(partial)
			return fmt.Errorf("install %s: %w", v, err)
		}
	}
	if err := nl.SetCurrent(v); err != nil {
		return err
	}
	if _, err := nl.Migrate(""); err != nil { // entry links
		return err
	}
	return nil
}

// rollback stops Ervisio (when it was started), starts LinuxAdmin again
// and removes what this transition created.
func (t *Transition) rollback(ctx context.Context, u *undo, started bool) {
	if started {
		_ = t.Systemd.Systemctl(ctx, "stop", brand.ServiceUnit)
		if err := t.Systemd.Systemctl(ctx, "start", brand.LegacyServiceUnit); err != nil {
			t.logf("start %s again: %v", brand.LegacyServiceUnit, err)
		} else if t.LegacyHealth != nil {
			if err := t.LegacyHealth(ctx); err != nil {
				t.logf("%s does not answer after the rollback: %v", brand.LegacyName, err)
			}
		}
	}
	Undo(t.Paths, u.imported)
	nl := t.newLayout()
	if u.unitNew {
		os.Remove(t.Paths.At(newUnitFile))
	}
	if u.libCreated {
		os.RemoveAll(nl.LibDir)
	} else {
		if u.prevCurrent != "" {
			_ = nl.SetCurrent(u.prevCurrent)
		} else {
			os.Remove(filepath.Join(nl.LibDir, "current"))
		}
		if u.versionNew && u.prevCurrent != t.Version {
			os.RemoveAll(nl.VersionDir(t.Version))
		}
		if u.bridgeLink {
			os.Remove(filepath.Join(nl.LibDir, brand.BridgeBinary))
		}
	}
	if u.binLinkNew {
		os.Remove(nl.BinLink)
	}
	_ = t.Systemd.Systemctl(ctx, "daemon-reload")
}

// failure records a transition that failed, so a daemon started from the
// LinuxAdmin layout does not retry the same version on every start.
type failure struct {
	Version string `json:"version"`
	At      int64  `json:"at"`
	Error   string `json:"error,omitempty"`
}

func failurePath(p Paths) string {
	return p.At(brand.LegacyStateDir + "/updates/" + brand.Slug + "-transition-failed.json")
}

func writeFailure(p Paths, v, msg string) {
	b, _ := json.Marshal(failure{Version: v, At: time.Now().UnixMilli(), Error: msg})
	_ = writeFileAtomic(failurePath(p), append(b, '\n'), 0o644)
}

// FailedBefore reports whether a transition to v failed already.
func FailedBefore(p Paths, v string) bool {
	b, err := os.ReadFile(failurePath(p))
	if err != nil {
		return false
	}
	var f failure
	return json.Unmarshal(b, &f) == nil && f.Version == v
}

// LegacyVersionOf returns the version folder when exe (resolved) is a
// binary in LinuxAdmin's versioned layout: /usr/lib/linuxadmin/versions/<v>/bin/*.
func LegacyVersionOf(p Paths, exe string) (string, bool) {
	bin := filepath.Dir(exe)
	vdir := filepath.Dir(bin)
	if filepath.Base(bin) != "bin" || filepath.Dir(vdir) != p.At(brand.LegacyLibDir+"/versions") {
		return "", false
	}
	v := filepath.Base(vdir)
	if !update.ValidDirName(v) {
		return "", false
	}
	return v, true
}

// TransitionFlag starts the transition helper:
//
//	ervisiod --transition <version> [--auto]
//
// LinuxAdmin's updater starts the new version's binary with
// update.HelperFlag instead; both end in RunHelper.
const TransitionFlag = "--transition"

// RunHelper is the entry point of the transition, as root, in a transient
// systemd unit. args are what follows --apply-update or --transition.
func RunHelper(args []string) int {
	logf := func(format string, a ...any) {
		fmt.Fprintf(os.Stderr, brand.DaemonBinary+" transition: "+format+"\n", a...)
	}
	if os.Geteuid() != 0 {
		logf("must run as root")
		return 2
	}
	target, auto := "", false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--kind" && i+1 < len(args):
			if args[i+1] != update.KindUpdate {
				// A rollback is LinuxAdmin's business: its own binary does it.
				logf("refusing --kind %s: run by %s", args[i+1], brand.LegacyName)
				return 2
			}
			i++
		case a == "--auto":
			auto = true
		case target == "" && !strings.HasPrefix(a, "-"):
			target = a
		default:
			logf("unexpected argument %q", a)
			return 2
		}
	}
	if !update.ValidDirName(target) {
		logf("usage: %s %s <version>", brand.DaemonBinary, TransitionFlag)
		return 2
	}
	p := Paths{}
	st := &update.State{Dir: p.At(brand.LegacyStateDir + "/updates")}
	var unlock func()
	var err error
	for i := 0; i < 30; i++ { // the updater releases its lock just before starting us
		if unlock, err = st.Lock(); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		logf("%v", err)
		return 1
	}
	defer unlock()
	t := &Transition{
		Paths:   p,
		Version: target,
		Auto:    auto,
		Systemd: Systemctl{},
		Health: func(ctx context.Context, want string) error {
			return update.NewHTTPHealth(brand.ConfigPath).Wait(ctx, want, update.HealthTimeout)
		},
		LegacyHealth: func(ctx context.Context) error {
			return update.NewHTTPHealth(brand.LegacyConfigPath).Wait(ctx, "", update.HealthTimeout)
		},
		Logf: logf,
	}
	if res := t.Run(context.Background()); res.State != update.StateOK {
		return 1
	}
	return 0
}

// StartTransition is called by a daemon that finds itself started from
// LinuxAdmin's layout (by linuxadmin.service, after LinuxAdmin's installer
// or a manual switch installed this version there): it starts the
// transition in a transient unit, unless a transition to this version
// failed before. The daemon keeps serving until the transition stops it.
func StartTransition(ctx context.Context, p Paths, exe, v string, launch update.Launcher) error {
	if FailedBefore(p, v) {
		return fmt.Errorf("the move to %s %s failed before (%s); run %s %s %s as root to try again", brand.Name, v, failurePath(p), exe, TransitionFlag, v)
	}
	if !exists(p.At(legacyUnitFile)) && !exists(p.At("/usr/lib/systemd/system/"+brand.LegacyServiceUnit)) {
		return errors.New("no " + brand.LegacyServiceUnit + " to move from")
	}
	unit := fmt.Sprintf("%s-transition-%s-%d", brand.Slug, strings.NewReplacer("+", "_", "~", "_").Replace(v), time.Now().Unix())
	return launch(ctx, unit, []string{exe, TransitionFlag, v})
}
