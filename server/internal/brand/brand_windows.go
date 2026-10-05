//go:build windows

package brand

import (
	"os"
	"path/filepath"
)

// Default locations on Windows. Configuration, certificates and state live
// under %ProgramData%\Ervisio; the web app, plugins and the bridge are
// installed next to ervisiod.exe (normally %ProgramFiles%\Ervisio). These
// are variables because they depend on the machine's environment.
var (
	// DataRoot is %ProgramData%\Ervisio.
	DataRoot = filepath.Join(programData(), Name)
	// InstallDir is the folder of ervisiod.exe.
	InstallDir = installDir()

	ConfigDir  = DataRoot
	ConfigPath = filepath.Join(ConfigDir, Slug+".conf")
	TLSDir     = filepath.Join(ConfigDir, "tls")

	// There is no versioned layout or package manager on Windows: LibDir is
	// the install folder and ManagedMarker names the (MSI/script) installer,
	// which keeps in-place self-update off.
	LibDir        = InstallDir
	ManagedMarker = filepath.Join(DataRoot, "managed")
	BinLink       = filepath.Join(InstallDir, DaemonBinary+".exe")

	StateDir   = filepath.Join(DataRoot, "data")
	UpdatesDir = filepath.Join(StateDir, "updates")
	RunDir     = filepath.Join(DataRoot, "run")
	CacheDir   = filepath.Join(DataRoot, "cache")

	WebDir              = filepath.Join(InstallDir, "web")
	PackagedPluginsDir  = filepath.Join(InstallDir, "plugins")
	InstalledPluginsDir = filepath.Join(StateDir, "plugins")
)

func programData() string {
	if d := os.Getenv("ProgramData"); d != "" {
		return d
	}
	if d := os.Getenv("ALLUSERSPROFILE"); d != "" {
		return d
	}
	return `C:\ProgramData`
}

func installDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return filepath.Join(os.Getenv("ProgramFiles"), Name)
}
