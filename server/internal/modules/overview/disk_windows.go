//go:build windows

package overview

import (
	"os"

	"github.com/ervisio/ervisio/server/internal/sys"
)

// diskUsage returns the used and available bytes of the volume at point.
func diskUsage(point string) (used, avail uint64, ok bool) {
	total, _, a, err := sys.DiskSpace(point)
	if err != nil || total == 0 {
		return 0, 0, false
	}
	return total - min(total, a), a, true
}

// fileOwner is not available on windows yet.
func fileOwner(os.FileInfo) (uint32, bool) { return 0, false }
