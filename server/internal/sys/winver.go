package sys

import (
	"strconv"
	"strings"
)

// windowsBrandColor is a neutral accent for Windows hosts.
const windowsBrandColor = "#4C9BE8"

// WindowsRelease builds an OSRelease from the registry values of
// HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion. ProductName still says
// "Windows 10" on Windows 11, so the build number decides (22000 and later).
func WindowsRelease(product, displayVersion, build string) OSRelease {
	name := strings.TrimSpace(product)
	if n, err := strconv.Atoi(strings.TrimSpace(build)); err == nil && n >= 22000 {
		name = strings.Replace(name, "Windows 10", "Windows 11", 1)
	}
	if name == "" {
		name = "Windows"
	}
	pretty := name
	if dv := strings.TrimSpace(displayVersion); dv != "" {
		pretty += " " + dv
	}
	return OSRelease{ID: "windows", Name: name, PrettyName: pretty, VersionID: strings.TrimSpace(displayVersion), Logo: windowsLogo(build)}
}

// windowsLogo picks the mark of the Windows generation from the build number,
// the same for client and Server editions: 22000+ is Windows 11 (and Server
// 2025), 10240+ Windows 10 (Server 2016-2022), 9200+ Windows 8 (Server 2012),
// anything older the classic flag.
func windowsLogo(build string) string {
	n, err := strconv.Atoi(strings.TrimSpace(build))
	switch {
	case err != nil:
		return "windows-11"
	case n >= 22000:
		return "windows-11"
	case n >= 10240:
		return "windows-10"
	case n >= 9200:
		return "windows-8"
	default:
		return "windows-7"
	}
}
