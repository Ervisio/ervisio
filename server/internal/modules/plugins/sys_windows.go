//go:build windows

package plugins

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/ervisio/ervisio/server/internal/winsec"
)

// fileUID is unknown on Windows, so ownership checks refuse.
func fileUID(fi fs.FileInfo) (int, bool) { return 0, false }

// renameAt renames oldName to newName, both single components inside the open
// folder dir. The folder's path is read from its handle (so a path swapped
// after the open is not used; the open handle also stops the folder itself
// from being renamed or deleted) and the rename replaces an existing file, or
// a link at the target, never following it.
func renameAt(dir *os.File, oldName, newName string) error {
	if oldName != filepath.Base(oldName) || newName != filepath.Base(newName) || newName == "." || newName == ".." {
		return errors.New("rename: not a plain name")
	}
	buf := make([]uint16, windows.MAX_PATH+1)
	for {
		n, err := windows.GetFinalPathNameByHandle(windows.Handle(dir.Fd()), &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return err
		}
		if int(n) < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]uint16, n+1)
	}
	d := windows.UTF16ToString(buf)
	return os.Rename(filepath.Join(d, oldName), filepath.Join(d, newName))
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

// platformCaller reads the user and groups from the process token (os/user
// needs a loaded profile, which a bridge started by the service may not
// have). Group names are the plain account names (Administrators,
// docker-users, ...). Members of Administrators are admins even when the
// group is deny-only in a filtered token: they can unlock administrator
// rights, like sudo/wheel members on Linux.
func platformCaller(c *caller) {
	tok := windows.GetCurrentProcessToken()
	if tok.IsElevated() {
		c.Admin = true
	}
	if tu, err := tok.GetTokenUser(); err == nil {
		if name, _, _, err := tu.User.Sid.LookupAccount(""); err == nil {
			c.Name = name
		}
	}
	tg, err := tok.GetTokenGroups()
	if err != nil {
		return
	}
	admins, _ := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	for _, g := range tg.AllGroups() {
		if admins != nil && g.Sid.Equals(admins) {
			c.Admin = true
		}
		if name, _, _, err := g.Sid.LookupAccount(""); err == nil && name != "" {
			c.Groups[name] = true
			c.Groups[strings.ToLower(name)] = true
		}
	}
}
