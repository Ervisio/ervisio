//go:build windows

package overview

import "os"

// diskUsage is not available on windows yet.
func diskUsage(string) (used, avail uint64, ok bool) { return 0, 0, false }

// fileOwner is not available on windows yet.
func fileOwner(os.FileInfo) (uint32, bool) { return 0, false }
