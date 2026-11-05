package legacy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// RemoveLegacy deletes what is left of LinuxAdmin once Ervisio runs: its
// programs (/usr/lib/linuxadmin, /usr/bin/linuxadmind, /usr/share/linuxadmin),
// the unit and drop-ins install.sh wrote, its PAM file when LinuxAdmin wrote
// it, and its cache. purge also deletes /etc/linuxadmin and
// /var/lib/linuxadmin; otherwise they stay, with the note. A packaged
// LinuxAdmin is refused (its package manager removes it), and so is a
// running one.
func RemoveLegacy(ctx context.Context, p Paths, sd Systemd, purge bool, logf func(string, ...any)) ([]string, error) {
	if skip, err := removeRefusal(ctx, p, sd, purge); skip || err != nil {
		return nil, err
	}
	var removed []string
	rm := func(path string, all bool) {
		if removePath(p, path, all, logf) {
			removed = append(removed, path)
		}
	}
	_ = sd.Systemctl(ctx, "disable", brand.LegacyServiceUnit)
	if fileHasPrefix(p.At(legacyUnitFile), LegacyInstallerUnitHeader) {
		rm(legacyUnitFile, false)
	}
	rm(legacyDropInDir, true)
	rm(brand.LegacyBinLink, false)
	rm(brand.LegacyLibDir, true)
	rm(brand.LegacyShareDir, true)
	rm("/var/cache/"+brand.LegacySlug, true)
	if fileContains(p.At(pamDir+"/"+brand.LegacyPAMService), legacyPAMHeader) {
		rm(pamDir+"/"+brand.LegacyPAMService, false)
	}
	if purge {
		rm(brand.LegacyConfigDir, true)
		rm(brand.LegacyStateDir, true)
	} else if err := WriteNote(p); err != nil && logf != nil {
		logf("note: %v", err)
	}
	_ = sd.Systemctl(ctx, "daemon-reload")
	return removed, nil
}

// removePath deletes p.At(path) if it exists and reports whether it did.
func removePath(p Paths, path string, all bool, logf func(string, ...any)) bool {
	full := p.At(path)
	if _, err := os.Lstat(full); err != nil {
		return false
	}
	var err error
	if all {
		err = os.RemoveAll(full)
	} else {
		err = os.Remove(full)
	}
	if err != nil {
		if logf != nil {
			logf("remove %s: %v", full, err)
		}
		return false
	}
	return true
}

func fileHasPrefix(path, prefix string) bool {
	b, err := os.ReadFile(path)
	return err == nil && bytes.HasPrefix(b, []byte(prefix))
}

func fileContains(path, sub string) bool {
	b, err := os.ReadFile(path)
	return err == nil && bytes.Contains(b, []byte(sub))
}

// removeRefusal checks what keeps RemoveLegacy from running: it returns an
// error when removal is refused, or skip when there is nothing to remove.
func removeRefusal(ctx context.Context, p Paths, sd Systemd, purge bool) (skip bool, err error) {
	if b, err := os.ReadFile(p.At(brand.LegacyLibDir + "/managed")); err == nil {
		by := strings.TrimSpace(string(b))
		return false, fmt.Errorf("%s was installed by a package manager (%s): remove the %s package with it", brand.LegacyName, by, brand.LegacySlug)
	}
	if sd.IsActive(ctx, brand.LegacyServiceUnit) {
		return false, fmt.Errorf("%s is running: stop it first (systemctl disable --now %s)", brand.LegacyServiceUnit, brand.LegacyServiceUnit)
	}
	if !Installed(p) && !exists(p.At(legacyUnitFile)) && !exists(p.At(brand.LegacyBinLink)) {
		return true, nil
	}
	if !purge && Pending(p) {
		return false, errors.New("the move to " + brand.Name + " has not finished; keep " + brand.LegacyName + "'s files for now")
	}
	return false, nil
}

// ImportOnStart is run by the daemon (as root) before it reads its
// configuration: when LinuxAdmin's data is here and was not imported yet,
// it is imported (final at once). It does nothing while a transition owns a
// marked import.
func ImportOnStart(p Paths, logf func(string, ...any)) (*Result, error) {
	if Pending(p) || !Installed(p) {
		return &Result{}, nil
	}
	res, err := (&Import{Paths: p, Logf: logf}).Run()
	if err != nil {
		return res, err
	}
	if res.Any() {
		if err := WriteNote(p); err != nil && logf != nil {
			logf("note: %v", err)
		}
	}
	return res, nil
}

// MigrateFlag is the installer's and the packages' command:
//
//	ervisiod --migrate-legacy
//
// It imports LinuxAdmin's data and moves the scheduled update; the caller
// then stops LinuxAdmin and removes its programs.
const MigrateFlag = "--migrate-legacy"

// RemoveFlag runs RemoveLegacy: ervisiod --remove-legacy [--purge].
const RemoveFlag = "--remove-legacy"

// RunMigrate is the entry point of MigrateFlag and RemoveFlag. It returns
// the exit code.
func RunMigrate(flag string, args []string) int {
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, brand.DaemonBinary+": "+flag+" is for Linux only")
		return 2
	}
	logf := func(format string, a ...any) { fmt.Printf(format+"\n", a...) }
	errf := func(format string, a ...any) { fmt.Fprintf(os.Stderr, brand.DaemonBinary+": "+format+"\n", a...) }
	if os.Geteuid() != 0 {
		errf("%s must run as root", flag)
		return 2
	}
	p := Paths{}
	switch flag {
	case MigrateFlag:
		return runMigrateData(p, args, logf, errf)
	case RemoveFlag:
		return runRemove(p, args, logf, errf)
	}
	return 2
}

func runMigrateData(p Paths, args []string, logf, errf func(string, ...any)) int {
	if len(args) > 0 {
		errf("unexpected argument %q", args[0])
		return 2
	}
	if !Installed(p) {
		logf("%s is not installed here: nothing to migrate.", brand.LegacyName)
		return 0
	}
	res, err := (&Import{Paths: p, Logf: logf}).Run()
	if err != nil {
		errf("%v", err)
		return 1
	}
	if !res.Any() {
		logf("%s's data was imported already.", brand.LegacyName)
	}
	if err := WriteNote(p); err != nil {
		errf("note: %v", err)
	}
	if err := MigrateSchedule(context.Background(), p, Systemctl{}, logf); err != nil {
		errf("scheduled update: %v", err)
	}
	return 0
}

func runRemove(p Paths, args []string, logf, errf func(string, ...any)) int {
	purge := false
	for _, a := range args {
		if a != "--purge" {
			errf("unexpected argument %q", a)
			return 2
		}
		purge = true
	}
	removed, err := RemoveLegacy(context.Background(), p, Systemctl{}, purge, errf)
	if err != nil {
		errf("%v", err)
		return 1
	}
	if len(removed) == 0 {
		logf("Nothing of %s is left to remove.", brand.LegacyName)
		return 0
	}
	for _, r := range removed {
		logf("removed %s", r)
	}
	if !purge && isDir(p.At(brand.LegacyConfigDir)) {
		logf("Kept %s and %s (--purge removes them).", brand.LegacyConfigDir, brand.LegacyStateDir)
	}
	return 0
}
