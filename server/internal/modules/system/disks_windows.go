//go:build windows

package system

import "errors"

// diskUsage is not implemented on windows yet. readDisks reads the Linux
// mount table, which does not exist there, so it reports no disks and this is
// not reached.
func diskUsage(path string) (total, used, avail uint64, err error) {
	return 0, 0, 0, errors.New("disk usage is not supported on windows yet")
}
