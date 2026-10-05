//go:build windows

package sys

// ReadOSRelease describes the running Windows from the registry.
func ReadOSRelease() OSRelease {
	r := WindowsRelease(RegString(`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "ProductName"),
		firstNonEmpty(RegString(`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "DisplayVersion"),
			RegString(`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "ReleaseId")),
		RegString(`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "CurrentBuild"))
	return r
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
