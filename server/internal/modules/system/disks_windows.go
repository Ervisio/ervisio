//go:build windows

package system

import "github.com/ervisio/ervisio/server/internal/sys"

// diskUsage returns the size, used and available bytes of the volume at path.
func diskUsage(path string) (total, used, avail uint64, err error) {
	total, _, avail, err = sys.DiskSpace(path)
	if err != nil {
		return 0, 0, 0, err
	}
	return total, total - min(total, avail), avail, nil
}
