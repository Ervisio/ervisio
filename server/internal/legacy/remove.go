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
	if b, err := os.ReadFile(p.At(brand.LegacyLibDir + "/managed")); err == nil {
		by := strings.TrimSpace(string(b))
		return nil, fmt.Errorf("%s was installed by a package manager (%s): remove the %s package with it", brand.LegacyName, by, brand.LegacySlug)
	}
	if sd.IsActive(ctx, brand.LegacyServiceUnit) {
		return nil, fmt.Errorf("%s is running: stop it first (systemctl disable --now %s)", brand.LegacyServiceUnit, brand.LegacyServiceUnit)
	}
	if !Installed(p) && !exists(p.At(legacyUnitFile)) && !exists(p.At(brand.LegacyBinLink)) {
		return nil, nil
	}
	if !purge && Pending(p) {
		return nil, errors.New("the move to " + brand.Name + " has not finished; keep " + brand.LegacyName + "'s files for now")
	}
	var removed []string
	rm := func(path string, all bool) {
		full := p.At(path)
		if _, err := os.Lstat(full); err != nil {
			return
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
			return
		}
		removed = append(removed, path)
	}
	_ = sd.Systemctl(ctx, "disable", brand.LegacyServiceUnit)
	if b, err := os.ReadFile(p.At(legacyUnitFile)); err == nil && bytes.HasPrefix(b, []byte(LegacyInstallerUnitHeader)) {
		rm(legacyUnitFile, false)
	}
	rm(legacyDropInDir, true)
	rm(brand.LegacyBinLink, false)
	rm(brand.LegacyLibDir, true)
	rm(brand.LegacyShareDir, true)
	rm("/var/cache/"+brand.LegacySlug, true)
	if b, err := os.ReadFile(p.At(pamDir + "/" + brand.LegacyPAMService)); err == nil && bytes.Contains(b, []byte(legacyPAMHeader)) {
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
	ctx := context.Background()
	p := Paths{}
	switch flag {
	case MigrateFlag:
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
		if err := MigrateSchedule(ctx, p, Systemctl{}, logf); err != nil {
			errf("scheduled update: %v", err)
		}
		return 0
	case RemoveFlag:
		purge := false
		for _, a := range args {
			if a != "--purge" {
				errf("unexpected argument %q", a)
				return 2
			}
			purge = true
		}
		removed, err := RemoveLegacy(ctx, p, Systemctl{}, purge, errf)
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
	return 2
}
