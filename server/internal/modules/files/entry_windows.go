//go:build windows

package files

import "os"

// setOwner leaves the owner fields at their zero values: Windows has no
// numeric uid/gid.
func setOwner(e *Entry, fi os.FileInfo) {}
