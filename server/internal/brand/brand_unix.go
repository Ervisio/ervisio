//go:build !windows

package brand

// Default locations on Linux (and other Unix systems).
const (
	// ConfigDir and ConfigPath are the system configuration locations.
	ConfigDir  = "/etc/" + Slug
	ConfigPath = ConfigDir + "/" + Slug + ".conf"
	// TLSDir holds the generated self-signed certificate.
	TLSDir = ConfigDir + "/tls"
	// LibDir holds the installed versions (LibDir/versions/<v>/{bin,web,plugins})
	// and the `current` / `previous` symlinks used by self-update. See
	// internal/update and docs/RELEASING.md.
	LibDir = "/usr/lib/" + Slug
	// ManagedMarker exists when a package manager installed Ervisio
	// (.deb, .rpm, PKGBUILD). It holds the manager's name ("apt", "dnf",
	// "zypper", "pacman"); self-update is then off, the package manager
	// installs new versions. Packages use the flat layout: BinLink is the
	// daemon itself, the bridge is LibDir/ervisio-bridge, the web app
	// and plugins are in /usr/share/ervisio.
	ManagedMarker = LibDir + "/managed"
	// BinLink is the command on PATH; with the versioned layout it is a
	// symlink to LibDir/current/bin/ervisiod.
	BinLink = "/usr/bin/" + DaemonBinary
	// StateDir keeps the daemon's state (installed plugins, update state).
	StateDir = "/var/lib/" + Slug
	// UpdatesDir keeps self-update state (last.json) and the root-only
	// download staging folder.
	UpdatesDir = StateDir + "/updates"
	// RunDir is the runtime folder (tmpfs) shared by the daemon and bridges.
	RunDir = "/run/" + Slug
	// CacheDir is the system cache folder.
	CacheDir = "/var/cache/" + Slug
	// WebDir is where the web app of a flat (pre-versioned) install lives.
	// A versioned install serves LibDir/versions/<v>/web instead.
	WebDir = "/usr/share/" + Slug + "/web"
	// PackagedPluginsDir and InstalledPluginsDir are the plugin roots. A
	// versioned install uses LibDir/versions/<v>/plugins as packaged root.
	PackagedPluginsDir  = "/usr/share/" + Slug + "/plugins"
	InstalledPluginsDir = StateDir + "/plugins"
)
