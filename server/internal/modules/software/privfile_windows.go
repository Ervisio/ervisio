//go:build windows

package software

import "os"

// No O_NOFOLLOW/O_NONBLOCK on windows.
const (
	oNoFollow = 0
	oNonblock = 0
)

// fileOwnerUID has no uid on windows; -1 never matches a trusted owner.
func fileOwnerUID(os.FileInfo) int { return -1 }
