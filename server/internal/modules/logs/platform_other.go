//go:build !windows

package logs

// onWindows selects the Windows Event Log backend instead of the systemd one.
const onWindows = false

// absLogPath reports whether p is an absolute path for this platform.
func absLogPath(p string) bool { return p != "" && p[0] == '/' }

// pathSeps are the path separators used to take the base name.
const pathSeps = "/"
