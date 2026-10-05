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
	return OSRelease{ID: "windows", Name: name, PrettyName: pretty, VersionID: strings.TrimSpace(displayVersion)}
}
