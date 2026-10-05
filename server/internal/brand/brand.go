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

	// UserDataDir is relative to the user's home directory.
	UserDataDir = ".config/" + Slug
	// PrefsFile is the per-user preferences file name inside UserDataDir.
	PrefsFile = "prefs.json"

	// ServiceUnit is the systemd unit of the daemon.
	ServiceUnit = Slug + ".service"
	// GitHubRepo is where releases are published.
	GitHubRepo = "ervisio/ervisio"
	// RepoURL is the project's home page.
	RepoURL = "https://github.com/" + GitHubRepo

	// PluginCatalogURL is the default marketplace catalog (config
	// plugins.catalog_url), built and signed by the Ervisio/plugins registry.
	// Its signature is at the same address with .sig instead of .json.
	PluginCatalogURL = "https://ervisio.github.io/plugins/catalog.json"

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
