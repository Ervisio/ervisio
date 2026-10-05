//go:build windows

package logs

import "os"

// fileInode is not available on windows yet; rotation is not detected.
func fileInode(os.FileInfo) (uint64, bool) { return 0, false }
