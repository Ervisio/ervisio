//go:build windows

package plugins

import (
	"errors"
	"io/fs"
	"os"
)

// fileUID is unknown on Windows, so ownership checks refuse.
func fileUID(fi fs.FileInfo) (int, bool) { return 0, false }

// renameAt is not implemented on Windows yet: refuse rather than rename
// by path outside the opened folder.
func renameAt(dir *os.File, oldName, newName string) error {
	return errors.New("renaming inside a folder is not supported on windows yet")
}
