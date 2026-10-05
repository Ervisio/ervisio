//go:build windows

package plugins

import (
	"errors"
	"io/fs"
	"os"

	"github.com/ervisio/ervisio/server/internal/winsec"
)

// fileUID is unknown on Windows, so ownership checks refuse.
func fileUID(fi fs.FileInfo) (int, bool) { return 0, false }

// renameAt is not implemented on Windows yet: refuse rather than rename
// by path outside the opened folder.
func renameAt(dir *os.File, oldName, newName string) error {
	return errors.New("renaming inside a folder is not supported on windows yet")
}

// socketOwnedByUs: there is no uid. The tunnel folders are made by the daemon
// and nobody else can write in them (ACLs), so a socket there is owned by
// SYSTEM, Administrators or the daemon's user; a socket owned by any other
// account is refused. The user's access is granted by the ACL itself.
func socketOwnedByUs(p string, _ fs.FileInfo) bool {
	in, err := winsec.InspectPath(p)
	if err != nil {
		return false
	}
	me, err := winsec.ProcessUser()
	if err != nil {
		return false
	}
	return winsec.SIDIn(in.Owner, winsec.System(), winsec.Administrators(), me)
}
