package update

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// Layout describes where Ervisio is installed:
//
//	LibDir/versions/<v>/{bin/ervisiod, bin/ervisio-bridge, web/, plugins/, packaging/, VERSION}
//	LibDir/current  -> versions/<v>   (the running version)
//	LibDir/previous -> versions/<v>   (kept for rollback)
//	LibDir/ervisio-bridge -> current/bin/ervisio-bridge   (compatibility with old unit files)
//	BinLink (/usr/bin/ervisiod) -> LibDir/current/bin/ervisiod
//
// The flat layout of earlier installs (a real /usr/bin/ervisiod,
// LibDir/ervisio-bridge, /usr/share/ervisio/{web,plugins}) is
// migrated by Migrate before the first update.
type Layout struct {
	LibDir  string
	BinLink string
	// Flat-layout locations (read by Migrate only).
	FlatWeb     string
	FlatPlugins string
	// Managed is the marker file written by distribution packages
	// (brand.ManagedMarker); see ManagedBy.
	Managed string
}

// DefaultLayout is the system layout.
func DefaultLayout() *Layout {
	return &Layout{LibDir: brand.LibDir, BinLink: brand.BinLink, FlatWeb: brand.WebDir, FlatPlugins: brand.PackagedPluginsDir,
		Managed: brand.ManagedMarker}
}

var managerRe = regexp.MustCompile(`^[a-z][a-z0-9._+-]{0,31}$`)

// ManagedBy returns the package manager that installed Ervisio, read
// from the marker file ("" when there is none: an install.sh or source
// install that updates itself). A marker whose content is not a plain name
// still counts and reads as "unknown".
func (l *Layout) ManagedBy() string {
	if l.Managed == "" {
		return ""
	}
	if _, err := os.Lstat(l.Managed); err != nil {
		return ""
	}
	b, err := readSmall(l.Managed, 256)
	if err != nil {
		return "unknown"
	}
	name := strings.TrimSpace(string(b))
	if !managerRe.MatchString(name) {
		return "unknown"
	}
	return name
}

// Kinds of installation.
const (
	KindVersioned = "versioned"
	KindFlat      = "flat"
	KindNone      = "none" // not installed (e.g. running from a build folder)
)

func (l *Layout) VersionsDir() string        { return filepath.Join(l.LibDir, "versions") }
func (l *Layout) currentLink() string        { return filepath.Join(l.LibDir, "current") }
func (l *Layout) previousLink() string       { return filepath.Join(l.LibDir, "previous") }
func (l *Layout) bridgeCompat() string       { return filepath.Join(l.LibDir, brand.BridgeBinary) }
func (l *Layout) VersionDir(v string) string { return filepath.Join(l.VersionsDir(), v) }

// DaemonPath / BridgePath are the binaries of an installed version.
func (l *Layout) DaemonPath(v string) string {
	return filepath.Join(l.VersionDir(v), "bin", brand.DaemonBinary)
}
func (l *Layout) BridgePath(v string) string {
	return filepath.Join(l.VersionDir(v), "bin", brand.BridgeBinary)
}

// Kind detects the installation kind.
func (l *Layout) Kind() string {
	if _, err := l.Current(); err == nil {
		return KindVersioned
	}
	if isRegular(l.BinLink) && isRegular(l.bridgeCompat()) {
		return KindFlat
	}
	return KindNone
}

// readVersionLink returns the version folder a link points to
// ("versions/<v>"), checking that it is a real folder.
func (l *Layout) readVersionLink(link string) (string, error) {
	t, err := os.Readlink(link)
	if err != nil {
		return "", err
	}
	v := filepath.Base(t)
	if filepath.Clean(t) != filepath.Join("versions", v) || !ValidDirName(v) {
		return "", fmt.Errorf("%s points to %q, not to versions/<version>", link, t)
	}
	fi, err := os.Lstat(l.VersionDir(v))
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s is not a folder", l.VersionDir(v))
	}
	return v, nil
}

// Current is the active version folder name.
func (l *Layout) Current() (string, error) { return l.readVersionLink(l.currentLink()) }

// Previous is the version kept for rollback ("" when there is none).
func (l *Layout) Previous() string {
	v, err := l.readVersionLink(l.previousLink())
	if err != nil {
		return ""
	}
	cur, _ := l.Current()
	if v == cur {
		return ""
	}
	return v
}

// Installed lists the version folders.
func (l *Layout) Installed() []string {
	ents, err := os.ReadDir(l.VersionsDir())
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() && ValidDirName(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// VersionFile reads versions/<v>/VERSION ("" when missing).
func (l *Layout) VersionFile(v string) string {
	b, err := readSmall(filepath.Join(l.VersionDir(v), "VERSION"), 256)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// setLink points link at target atomically: a temporary symlink renamed
// over the old one, so readers always see either the old or the new target.
func setLink(link, target string) error {
	tmp := filepath.Join(filepath.Dir(link), "."+filepath.Base(link)+".tmp-"+randHex(6))
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		os.Remove(tmp)
		return err
	}
	return syncDir(filepath.Dir(link))
}

// SetCurrent switches the active version (atomic).
func (l *Layout) SetCurrent(v string) error {
	if !ValidDirName(v) || !isRegular(l.DaemonPath(v)) {
		return fmt.Errorf("version %q is not installed", v)
	}
	return setLink(l.currentLink(), filepath.Join("versions", v))
}

// SetPrevious records the rollback version ("" removes the link).
func (l *Layout) SetPrevious(v string) error {
	if v == "" {
		err := os.Remove(l.previousLink())
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if !ValidDirName(v) {
		return fmt.Errorf("invalid version %q", v)
	}
	return setLink(l.previousLink(), filepath.Join("versions", v))
}

// ensureEntryLinks makes BinLink and the bridge compatibility path follow
// `current` (replacing the regular files of a flat install atomically).
func (l *Layout) ensureEntryLinks() error {
	want := filepath.Join(l.LibDir, "current", "bin", brand.DaemonBinary)
	if t, err := os.Readlink(l.BinLink); err != nil || t != want {
		if err := os.MkdirAll(filepath.Dir(l.BinLink), 0o755); err != nil {
			return err
		}
		if err := setLink(l.BinLink, want); err != nil {
			return fmt.Errorf("link %s: %w", l.BinLink, err)
		}
	}
	wantB := filepath.Join("current", "bin", brand.BridgeBinary)
	if t, err := os.Readlink(l.bridgeCompat()); err != nil || t != wantB {
		if err := setLink(l.bridgeCompat(), wantB); err != nil {
			return fmt.Errorf("link %s: %w", l.bridgeCompat(), err)
		}
	}
	return nil
}

// Migrate turns a flat install into the versioned layout without changing
// what runs: the flat binaries, web app and packaged plugins are copied
// into versions/<name>, `current` points there and the entry points become
// symlinks. name is the running version's folder name. The flat
// /usr/share/ervisio folders stay (an old binary rolled back to still
// reads them). Migrating a versioned install only repairs the entry links.
func (l *Layout) Migrate(name string) (string, error) {
	switch l.Kind() {
	case KindVersioned:
		cur, _ := l.Current()
		return cur, l.ensureEntryLinks()
	case KindNone:
		return "", errors.New("Ervisio is not installed in " + l.LibDir)
	}
	if !ValidDirName(name) {
		name = "legacy"
	}
	if err := os.MkdirAll(l.VersionsDir(), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Lstat(l.VersionDir(name)); err == nil {
		name += "-flat"
		os.RemoveAll(l.VersionDir(name))
	}
	tmp := filepath.Join(l.VersionsDir(), "."+name+".partial-"+randHex(6))
	err := func() error {
		if err := os.MkdirAll(filepath.Join(tmp, "bin"), 0o755); err != nil {
			return err
		}
		if err := copyFile(l.BinLink, filepath.Join(tmp, "bin", brand.DaemonBinary), 0o755); err != nil {
			return err
		}
		if err := copyFile(l.bridgeCompat(), filepath.Join(tmp, "bin", brand.BridgeBinary), 0o755); err != nil {
			return err
		}
		for src, dst := range map[string]string{l.FlatWeb: "web", l.FlatPlugins: "plugins"} {
			if fi, err := os.Lstat(src); err == nil && fi.IsDir() {
				if err := copyTree(src, filepath.Join(tmp, dst)); err != nil {
					return err
				}
			}
		}
		if err := os.WriteFile(filepath.Join(tmp, "VERSION"), []byte(name+"\n"), 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, l.VersionDir(name))
	}()
	if err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("migrate flat install: %w", err)
	}
	if err := l.SetCurrent(name); err != nil {
		return "", err
	}
	if err := l.ensureEntryLinks(); err != nil {
		return "", err
	}
	return name, nil
}

// InstallFrom moves an extracted release folder (on the same file system,
// e.g. a versions/.partial-* folder) to versions/<v>, replacing a stale
// folder of the same name unless it is the current version.
func (l *Layout) InstallFrom(dir, v string) error {
	if !ValidDirName(v) {
		return fmt.Errorf("invalid version %q", v)
	}
	if cur, _ := l.Current(); cur == v {
		return fmt.Errorf("version %s is the running version", v)
	}
	dst := l.VersionDir(v)
	if _, err := os.Lstat(dst); err == nil {
		old := filepath.Join(l.VersionsDir(), "."+v+".old-"+randHex(6))
		if err := os.Rename(dst, old); err != nil {
			return err
		}
		defer os.RemoveAll(old)
	}
	if err := os.Rename(dir, dst); err != nil {
		return err
	}
	return syncDir(l.VersionsDir())
}

// Prune removes every version folder except current, previous and keep,
// and leftovers of interrupted installs.
func (l *Layout) Prune(keep ...string) []string {
	cur, _ := l.Current()
	k := map[string]bool{cur: true, l.Previous(): true}
	for _, v := range keep {
		k[v] = true
	}
	ents, err := os.ReadDir(l.VersionsDir())
	if err != nil {
		return nil
	}
	var removed []string
	for _, e := range ents {
		n := e.Name()
		if k[n] && n != "" {
			continue
		}
		if !strings.HasPrefix(n, ".") && !ValidDirName(n) {
			continue // not ours
		}
		if err := os.RemoveAll(filepath.Join(l.VersionsDir(), n)); err == nil {
			removed = append(removed, n)
		}
	}
	return removed
}

// NewPartialDir returns a fresh (non-existing) path in versions/ to
// extract a release into, on the same file system as its destination.
func (l *Layout) NewPartialDir(v string) (string, error) {
	if err := os.MkdirAll(l.VersionsDir(), 0o755); err != nil {
		return "", err
	}
	return filepath.Join(l.VersionsDir(), "."+v+".partial-"+randHex(6)), nil
}

// RunningVersionDir returns the versions/<v> folder of the running
// executable when it was started from the versioned layout, so the daemon
// serves the web app and packaged plugins of its own version.
func RunningVersionDir() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", false
	}
	if d, ok := versionDirOf(exe, brand.LibDir); ok {
		return d, true
	}
	// The compatibility archive run from LinuxAdmin's layout, until the
	// move to Ervisio (internal/legacy) has happened.
	return versionDirOf(exe, brand.LegacyLibDir)
}

func versionDirOf(exe, libDir string) (string, bool) {
	bin := filepath.Dir(exe)
	vdir := filepath.Dir(bin)
	if filepath.Base(bin) != "bin" || filepath.Dir(vdir) != filepath.Join(libDir, "versions") || !ValidDirName(filepath.Base(vdir)) {
		return "", false
	}
	return vdir, true
}

// helpers

func isRegular(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().IsRegular()
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	_ = d.Sync()
	return nil
}

func readSmall(p string, max int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s is too large", p)
	}
	return b, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyTree copies folders and regular files (symlinks and special files
// are skipped).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			fi, err := d.Info()
			if err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if fi.Mode()&0o111 != 0 {
				mode = 0o755
			}
			return copyFile(p, target, mode)
		}
		return nil
	})
}
