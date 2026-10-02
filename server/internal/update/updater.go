// Package update implements LinuxAdmin's self-update: finding the latest
// GitHub release, downloading and verifying it (ed25519 signature over
// SHA256SUMS + sha256 of the archive), extracting it safely into
// /usr/lib/linuxadmin/versions/<v>, and switching the `current` symlink
// from a transient systemd unit that restarts the daemon and rolls back
// when the new version does not answer. See docs/RELEASING.md.
package update

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/brand"
)

// Event is one progress step of Apply, streamed to the UI.
type Event struct {
	// Phase: check | download | verify | extract | test | install | restart
	Phase   string `json:"phase"`
	File    string `json:"file,omitempty"`
	Done    int64  `json:"done,omitempty"`
	Total   int64  `json:"total,omitempty"`
	Percent int    `json:"percent,omitempty"`
	Version string `json:"version,omitempty"`
	Unit    string `json:"unit,omitempty"`
}

// Errors callers map to API codes.
var (
	ErrBusy         = errors.New("an update is already in progress")
	ErrPackages     = errors.New("a package transaction is running; try again when it has finished")
	ErrUpToDate     = errors.New("LinuxAdmin is already up to date")
	ErrChanged      = errors.New("the latest release changed since it was shown; check again")
	ErrNoPrevious   = errors.New("there is no previous version to roll back to")
	ErrNotInstalled = errors.New("this copy of LinuxAdmin was not installed in " + brand.LibDir + "; update it the way it was installed")
	ErrNoBuild      = errors.New("the release has no build for this architecture")
	ErrManaged      = errors.New("LinuxAdmin was installed by a package manager, which installs its updates")
)

// managedErr names the package manager in ErrManaged.
func managedErr(by string) error {
	return fmt.Errorf("%w (%s)", ErrManaged, by)
}

// Launcher starts the switch helper outside the daemon's service.
type Launcher func(ctx context.Context, unit string, argv []string) error

// systemdVersion is the major version printed by `systemd-run --version`
// ("systemd 219"), or 0 when it cannot be read.
func systemdVersion(ctx context.Context) int {
	out, err := exec.CommandContext(ctx, "systemd-run", "--version").Output()
	if err != nil {
		return 0
	}
	f := strings.Fields(string(out))
	if len(f) < 2 || f[0] != "systemd" {
		return 0
	}
	n, _ := strconv.Atoi(f[1])
	return n
}

// SystemdRun starts argv as a transient systemd service.
func SystemdRun(ctx context.Context, unit string, argv []string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"--unit=" + unit}
	// --collect (systemd 236) removes the unit even when it fails; older
	// systemd (Amazon Linux 2 has 219) refuses the option.
	if systemdVersion(ctx) >= 236 {
		args = append(args, "--collect")
	}
	args = append(append(args, "--quiet", "--description="+brand.Name+" update", "--"), argv...)
	cmd := exec.CommandContext(ctx, "systemd-run", args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	cmd.Dir = "/"
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemd-run: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Updater prepares a new version and hands off to the switch helper.
type Updater struct {
	Layout   *Layout
	State    *State
	Checker  *Checker
	Download *Downloader
	Keys     []ed25519.PublicKey
	Arch     string
	Probe    Prober
	Launch   Launcher
	// Current is the running version (brand.Version).
	Current string
	Limits  Limits
}

// New returns an updater for this machine.
func New(checker *Checker) *Updater {
	return &Updater{
		Layout:   DefaultLayout(),
		State:    DefaultState(brand.UpdatesDir),
		Checker:  checker,
		Download: NewDownloader(),
		Keys:     TrustedKeys,
		Arch:     Arch,
		Probe:    ProbeBinary,
		Launch:   SystemdRun,
		Current:  brand.Version,
		Limits:   DefaultLimits,
	}
}

// Supported says whether this installation can update itself, and why not.
// exe is the running executable (resolved), dev the daemon's --dev mode.
func (u *Updater) Supported(exe string, dev bool) (bool, string) {
	if dev {
		return false, "the daemon runs in development mode"
	}
	if by := u.Layout.ManagedBy(); by != "" {
		return false, managedErr(by).Error()
	}
	if u.Layout.Kind() == KindNone {
		return false, ErrNotInstalled.Error()
	}
	inLib := strings.HasPrefix(exe, filepath.Clean(u.Layout.LibDir)+string(filepath.Separator))
	if exe != "" && !inLib && exe != u.Layout.BinLink {
		return false, fmt.Sprintf("this copy runs from %s, not from %s; update it the way it was installed", exe, u.Layout.LibDir)
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return false, "systemd-run is not available"
	}
	return true, ""
}

// Apply downloads, verifies and installs the latest release of channel,
// then starts the switch helper. want, when not empty, must equal the
// latest version (what the user confirmed). It returns the version and the
// transient unit name; the daemon restarts shortly after.
func (u *Updater) Apply(ctx context.Context, channel, want string, auto bool, progress func(Event)) (string, string, error) {
	if progress == nil {
		progress = func(Event) {}
	}
	if by := u.Layout.ManagedBy(); by != "" {
		return "", "", managedErr(by)
	}
	if u.Layout.Kind() == KindNone {
		return "", "", ErrNotInstalled
	}
	if u.State.Running() {
		return "", "", ErrBusy
	}
	if u.State.PackageTransactionRunning() {
		return "", "", ErrPackages
	}
	if err := u.State.Prepare(); err != nil {
		return "", "", fmt.Errorf("prepare %s: %w", u.State.Dir, err)
	}
	unlock, err := u.State.Lock()
	if err != nil {
		return "", "", ErrBusy
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()

	progress(Event{Phase: "check"})
	rel, _, err := u.Checker.Latest(ctx, channel, false)
	if err != nil {
		return "", "", err
	}
	v := rel.Version()
	if want != "" && DirName(want) != v {
		return "", "", ErrChanged
	}
	if !Newer(v, u.Current) {
		return "", "", ErrUpToDate
	}
	archive := ArchiveName(v, u.Arch)
	aArch, aSums, aSig := rel.Asset(archive), rel.Asset(SumsFile), rel.Asset(SigFile)
	if aArch == nil {
		return "", "", fmt.Errorf("%w (%s missing)", ErrNoBuild, archive)
	}
	if aSums == nil || aSig == nil {
		return "", "", errors.New("the release is not signed (" + SumsFile + " or " + SigFile + " missing)")
	}
	if aArch.Size > MaxArchive {
		return "", "", fmt.Errorf("%s is %d bytes, more than the %d allowed", archive, aArch.Size, MaxArchive)
	}

	stage, err := os.MkdirTemp(u.State.StagingDir(), v+"-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(stage)

	progress(Event{Phase: "download", File: SumsFile})
	sumsPath, sigPath, arcPath := filepath.Join(stage, SumsFile), filepath.Join(stage, SigFile), filepath.Join(stage, archive)
	if err := u.Download.Fetch(ctx, aSums.URL, sumsPath, maxSumsFile, nil); err != nil {
		return "", "", err
	}
	if err := u.Download.Fetch(ctx, aSig.URL, sigPath, maxSigFile, nil); err != nil {
		return "", "", err
	}
	progress(Event{Phase: "verify", File: SumsFile})
	sumsRaw, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", "", err
	}
	sigRaw, err := os.ReadFile(sigPath)
	if err != nil {
		return "", "", err
	}
	if err := VerifySums(sumsRaw, sigRaw, u.Keys); err != nil {
		return "", "", err
	}
	sums, err := ParseSums(sumsRaw)
	if err != nil {
		return "", "", err
	}
	if _, ok := sums[archive]; !ok {
		return "", "", fmt.Errorf("%s is not listed in the signed %s", archive, SumsFile)
	}

	err = u.Download.Fetch(ctx, aArch.URL, arcPath, MaxArchive, func(done, total int64) {
		if total <= 0 {
			total = aArch.Size
		}
		pct := 0
		if total > 0 {
			pct = int(done * 100 / total)
		}
		progress(Event{Phase: "download", File: archive, Done: done, Total: total, Percent: pct})
	})
	if err != nil {
		return "", "", err
	}
	progress(Event{Phase: "verify", File: archive})
	if err := VerifyFile(sums, archive, arcPath); err != nil {
		return "", "", err
	}

	progress(Event{Phase: "extract"})
	partial, err := u.Layout.NewPartialDir(v)
	if err != nil {
		return "", "", err
	}
	installed := false
	defer func() {
		if !installed {
			os.RemoveAll(partial)
		}
	}()
	lim := u.Limits
	if lim.MaxEntries == 0 {
		lim = DefaultLimits
	}
	if err := ExtractTarGz(arcPath, partial, ArchivePrefix(v, u.Arch), lim); err != nil {
		return "", "", err
	}
	if err := checkTree(partial, v); err != nil {
		return "", "", err
	}

	progress(Event{Phase: "test"})
	for _, bin := range []string{brand.DaemonBinary, brand.BridgeBinary} {
		got, err := u.Probe(ctx, filepath.Join(partial, "bin", bin))
		if err != nil {
			return "", "", fmt.Errorf("the new %s does not run on this machine: %v", bin, err)
		}
		if DirName(got) != v {
			return "", "", fmt.Errorf("the new %s reports version %q, expected %s", bin, got, v)
		}
	}

	progress(Event{Phase: "install", Version: v})
	if u.Layout.Kind() == KindFlat {
		if _, err := u.Layout.Migrate(DirName(u.Current)); err != nil {
			return "", "", err
		}
	} else if _, err := u.Layout.Migrate(""); err != nil { // repairs entry links
		return "", "", err
	}
	if err := u.Layout.InstallFrom(partial, v); err != nil {
		return "", "", fmt.Errorf("install %s: %w", v, err)
	}
	installed = true

	unit := UnitName(v)
	argv := []string{u.Layout.DaemonPath(v), HelperFlag, v, "--kind", KindUpdate}
	if auto {
		argv = append(argv, "--auto")
	}
	unlock()
	locked = false
	progress(Event{Phase: "restart", Version: v, Unit: unit})
	if err := u.Launch(ctx, unit, argv); err != nil {
		return "", "", err
	}
	return v, unit, nil
}

// Rollback starts the switch helper towards the previous version. The
// helper is the running version's binary (an old build may not have it).
func (u *Updater) Rollback(ctx context.Context, want string) (string, string, error) {
	if by := u.Layout.ManagedBy(); by != "" {
		return "", "", managedErr(by)
	}
	if u.Layout.Kind() != KindVersioned {
		return "", "", ErrNoPrevious
	}
	prev := u.Layout.Previous()
	if prev == "" {
		return "", "", ErrNoPrevious
	}
	if want != "" && want != prev {
		return "", "", ErrChanged
	}
	if u.State.Running() {
		return "", "", ErrBusy
	}
	if u.State.PackageTransactionRunning() {
		return "", "", ErrPackages
	}
	cur, err := u.Layout.Current()
	if err != nil {
		return "", "", err
	}
	if err := u.State.Prepare(); err != nil {
		return "", "", err
	}
	unit := UnitName(prev)
	argv := []string{u.Layout.DaemonPath(cur), HelperFlag, prev, "--kind", KindRollback}
	if err := u.Launch(ctx, unit, argv); err != nil {
		return "", "", err
	}
	return prev, unit, nil
}

// UnitName is the transient unit of a switch to v.
func UnitName(v string) string {
	s := strings.NewReplacer("+", "_", "~", "_").Replace(v)
	return fmt.Sprintf("%s-update-%s-%d", brand.Slug, s, time.Now().Unix())
}

// checkTree verifies an extracted release has what we need.
func checkTree(dir, v string) error {
	for _, f := range []string{"bin/" + brand.DaemonBinary, "bin/" + brand.BridgeBinary, "web/index.html", "VERSION"} {
		if !isRegular(filepath.Join(dir, f)) {
			return fmt.Errorf("the release archive has no %s", f)
		}
	}
	b, err := readSmall(filepath.Join(dir, "VERSION"), 256)
	if err != nil {
		return err
	}
	if got := DirName(strings.TrimSpace(string(b))); got != v {
		return fmt.Errorf("the archive is version %q, expected %s", got, v)
	}
	return nil
}
