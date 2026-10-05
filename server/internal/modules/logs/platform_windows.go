//go:build windows

package logs

import "path/filepath"

// onWindows selects the Windows Event Log backend instead of the systemd one.
const onWindows = true

// absLogPath reports whether p is an absolute path for this platform.
func absLogPath(p string) bool { return filepath.IsAbs(p) }

// pathSeps are the path separators used to take the base name.
const pathSeps = `/\`
