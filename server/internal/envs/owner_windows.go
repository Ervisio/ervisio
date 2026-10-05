//go:build windows

package envs

import "os"

// fileOwner is not supported on windows: the owner is never known, so the
// ownership checks fail.
func fileOwner(fi os.FileInfo) (uid, gid int, ok bool) { return 0, 0, false }
