//go:build windows

package legacy

import "io/fs"

// oNoFollow has no open flag on Windows; the LinuxAdmin move never runs there.
const oNoFollow = 0

// fileOwner is unknown on Windows.
func fileOwner(fi fs.FileInfo) (uid, gid int, ok bool) { return 0, 0, false }
