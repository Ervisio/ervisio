// Package brand holds the product name and every identifier derived from it,
// so that a rename touches a single file on the server side.
package brand

// Name is the product name shown to users.
const Name = "Ervisio"

// Version is the build version. It is overridden at link time with
// -ldflags "-X github.com/ervisio/ervisio/server/internal/brand.Version=…".
var Version = "0.1.0-dev"

// Derived identifiers.
const (
	// Slug is the lower-case identifier used in paths and binary names.
	Slug = "ervisio"

	DaemonBinary = Slug + "d"
	BridgeBinary = Slug + "-bridge"

	// ConfigDir and ConfigPath are the system configuration locations.
	ConfigDir  = "/etc/" + Slug
	ConfigPath = ConfigDir + "/" + Slug + ".conf"
	// TLSDir holds the generated self-signed certificate.
	TLSDir = ConfigDir + "/tls"

	// UserDataDir is relative to the user's home directory.
	UserDataDir = ".config/" + Slug
	// PrefsFile is the per-user preferences file name inside UserDataDir.
	PrefsFile = "prefs.json"

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
	// ServiceUnit is the systemd unit of the daemon.
	ServiceUnit = Slug + ".service"
	// StateDir keeps the daemon's state (installed plugins, update state).
	StateDir = "/var/lib/" + Slug
	// UpdatesDir keeps self-update state (last.json) and the root-only
	// download staging folder.
	UpdatesDir = StateDir + "/updates"
	// RunDir is the runtime folder (tmpfs) shared by the daemon and bridges.
	RunDir = "/run/" + Slug
	// CacheDir is the system cache folder.
	CacheDir = "/var/cache/" + Slug
	// GitHubRepo is where releases are published.
	GitHubRepo = "ervisio/ervisio"
	// RepoURL is the project's home page.
	RepoURL = "https://github.com/" + GitHubRepo

	// WebDir is where the web app of a flat (pre-versioned) install lives.
	// A versioned install serves LibDir/versions/<v>/web instead.
	WebDir = "/usr/share/" + Slug + "/web"
	// PackagedPluginsDir and InstalledPluginsDir are the plugin roots. A
	// versioned install uses LibDir/versions/<v>/plugins as packaged root.
	PackagedPluginsDir  = "/usr/share/" + Slug + "/plugins"
	InstalledPluginsDir = StateDir + "/plugins"

	// PAMService is the PAM service name; PAMFallbackService is used when
	// /etc/pam.d/<PAMService> does not exist.
	PAMService         = Slug
	PAMFallbackService = "login"

	// SessionCookie is the name of the session cookie.
	SessionCookie = "ervisio_session"
	// CSRFHeader / CSRFValue must accompany every state-changing request.
	CSRFHeader = "X-Requested-With"
	CSRFValue  = Slug
)

// The product was called LinuxAdmin up to version 0.2.0. These are the
// identifiers of that name, used only to migrate an existing installation
// (internal/legacy) and to stay compatible with what was already signed or
// sent under it. Never use them for anything new.
const (
	LegacyName         = "LinuxAdmin"
	LegacySlug         = "linuxadmin"
	LegacyDaemonBinary = LegacySlug + "d"
	LegacyBridgeBinary = LegacySlug + "-bridge"
	LegacyConfigDir    = "/etc/" + LegacySlug
	LegacyConfigPath   = LegacyConfigDir + "/" + LegacySlug + ".conf"
	LegacyLibDir       = "/usr/lib/" + LegacySlug
	LegacyBinLink      = "/usr/bin/" + LegacyDaemonBinary
	LegacyServiceUnit  = LegacySlug + ".service"
	LegacyStateDir     = "/var/lib/" + LegacySlug
	LegacyShareDir     = "/usr/share/" + LegacySlug
	LegacyUserDataDir  = ".config/" + LegacySlug
	LegacyPAMService   = LegacySlug
	// LegacyCSRFValue is still accepted for one release, so a browser tab
	// that loaded the LinuxAdmin web app keeps working until it reloads.
	LegacyCSRFValue = LegacySlug
	// LegacyGitHubRepo is where LinuxAdmin was published; GitHub redirects
	// it to GitHubRepo after the transfer.
	LegacyGitHubRepo = "Fonlogen/LinuxAdmin"
)
