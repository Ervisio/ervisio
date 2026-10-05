// Package legacy moves an installation of LinuxAdmin (the product's name up
// to version 0.2.0) to Ervisio. Nothing is moved: the LinuxAdmin files are
// copied to the Ervisio locations and left where they are, so going back to
// LinuxAdmin stays possible until they are removed (RemoveLegacy).
//
//	/etc/linuxadmin/linuxadmin.conf      -> /etc/ervisio/ervisio.conf (with tls/ and the rest)
//	/var/lib/linuxadmin                  -> /var/lib/ervisio (plugins, plugin state, update state)
//	/etc/pam.d/linuxadmin                -> /etc/pam.d/ervisio
//	/etc/systemd/system/linuxadmin.service.d/*.conf -> ervisio.service.d/
//	/etc/systemd/system/linuxadmin-update.{timer,service} -> ervisio-update.* (scheduled updates)
//	~/.config/linuxadmin                 -> ~/.config/ervisio (each user, by the user's bridge)
//
// Three ways lead here, all ending in the same steps (docs/RELEASING.md,
// "Rename transition"):
//
//   - Self-update from LinuxAdmin: its updater installs the compatibility
//     archive into /usr/lib/linuxadmin/versions/<v> and starts that
//     version's daemon binary as the switch helper. That binary is Ervisio:
//     it notices it was started from the LinuxAdmin layout and runs
//     Transition instead of a switch.
//   - install.sh and the distribution packages: ervisiod --migrate-legacy
//     (Import + scheduled updates), then they remove LinuxAdmin's programs.
//   - Anything else: ervisiod imports the data on start when /etc/ervisio
//     does not exist yet (ImportOnStart), and a daemon started from the
//     LinuxAdmin layout (an old install.sh, a manual switch) launches
//     Transition in a transient unit (StartTransition).
package legacy

import (
	"path/filepath"
	"runtime"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// Paths places every location under Root ("" is the real system), so tests
// can run the whole migration in a temporary folder.
type Paths struct{ Root string }

// At returns an absolute system path under Root.
func (p Paths) At(abs string) string {
	if p.Root == "" {
		return abs
	}
	return filepath.Join(p.Root, abs)
}

// System locations used by the migration.
const (
	systemdDir      = "/etc/systemd/system"
	pamDir          = "/etc/pam.d"
	legacyTimer     = brand.LegacySlug + "-update.timer"
	legacyTimerSvc  = brand.LegacySlug + "-update.service"
	newTimer        = brand.Slug + "-update.timer"
	newTimerSvc     = brand.Slug + "-update.service"
	legacyUnitFile  = systemdDir + "/" + brand.LegacyServiceUnit
	newUnitFile     = systemdDir + "/" + brand.ServiceUnit
	legacyDropInDir = legacyUnitFile + ".d"
	newDropInDir    = newUnitFile + ".d"

	// importMarker is left in a folder copied by an Import with Mark set,
	// until Finalize removes it: a folder that still has it is an
	// unfinished import, which the next Import replaces.
	importMarker = ".imported-from-" + brand.LegacySlug

	// NoteFile is written into the LinuxAdmin configuration folder once the
	// data lives in the Ervisio locations.
	NoteFile = "MOVED-TO-ERVISIO.txt"

	// InstallerUnitHeader is the first line install.sh writes into the unit
	// files it installs (packages and install.sh recognise it).
	InstallerUnitHeader = "# Installed by the Ervisio installer (install.sh); removed by install.sh --uninstall."
	// LegacyInstallerUnitHeader is the same line written by LinuxAdmin's installer.
	LegacyInstallerUnitHeader = "# Installed by the LinuxAdmin installer (install.sh); removed by install.sh --uninstall."
)

// Installed reports whether LinuxAdmin's configuration, state or programs
// are present.
func Installed(p Paths) bool {
	if runtime.GOOS == "windows" {
		return false // LinuxAdmin never ran on Windows: nothing to migrate
	}
	for _, d := range []string{brand.LegacyConfigDir, brand.LegacyStateDir, brand.LegacyLibDir} {
		if isDir(p.At(d)) {
			return true
		}
	}
	return false
}
