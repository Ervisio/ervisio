// Package brand holds the product name and every identifier derived from it,
// so that a rename touches a single file on the server side.
package brand

// Name is the product name shown to users.
const Name = "LinuxAdmin"

// Version is the build version. It is overridden at link time with
// -ldflags "-X github.com/Fonlogen/LinuxAdmin/server/internal/brand.Version=…".
var Version = "0.1.0-dev"

// Derived identifiers.
const (
	// Slug is the lower-case identifier used in paths and binary names.
	Slug = "linuxadmin"

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
	// BinLink is the command on PATH; with the versioned layout it is a
	// symlink to LibDir/current/bin/linuxadmind.
	BinLink = "/usr/bin/" + DaemonBinary
	// ServiceUnit is the systemd unit of the daemon.
	ServiceUnit = Slug + ".service"
	// UpdatesDir keeps self-update state (last.json) and the root-only
	// download staging folder.
	UpdatesDir = "/var/lib/" + Slug + "/updates"
	// GitHubRepo is where releases are published.
	GitHubRepo = "Fonlogen/LinuxAdmin"

	// WebDir is where the web app of a flat (pre-versioned) install lives.
	// A versioned install serves LibDir/versions/<v>/web instead.
	WebDir = "/usr/share/" + Slug + "/web"
	// PackagedPluginsDir and InstalledPluginsDir are the plugin roots. A
	// versioned install uses LibDir/versions/<v>/plugins as packaged root.
	PackagedPluginsDir  = "/usr/share/" + Slug + "/plugins"
	InstalledPluginsDir = "/var/lib/" + Slug + "/plugins"

	// PAMService is the PAM service name; PAMFallbackService is used when
	// /etc/pam.d/<PAMService> does not exist.
	PAMService         = Slug
	PAMFallbackService = "login"

	// SessionCookie is the name of the session cookie.
	SessionCookie = "la_session"
	// CSRFHeader / CSRFValue must accompany every state-changing request.
	CSRFHeader = "X-Requested-With"
	CSRFValue  = Slug
)
