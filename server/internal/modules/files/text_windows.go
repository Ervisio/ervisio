//go:build windows

package files

import (
	"os"
	"path/filepath"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// writeTextFile saves data by writing a temp file in the same folder and
// renaming it over path (MOVEFILE_REPLACE_EXISTING | WRITE_THROUGH). A link at
// path is refused, so saving never writes through a link.
func writeTextFile(path string, data []byte, expected int64) (any, error) {
	fi, link, err := lstatLink(path)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, pathErr("lstat", path, err)
	}
	if exists {
		if link {
			return nil, rpc.Errorf(rpc.Invalid, "%s is a link. Open the file it points to instead.", filepath.Base(path))
		}
		if !fi.Mode().IsRegular() {
			return nil, rpc.Errorf(rpc.Invalid, "Only regular files can be saved as text.")
		}
		if mt := fi.ModTime().UnixMilli(); expected != 0 && mt != expected {
			return nil, rpc.Errorf(rpc.Conflict, "This file was changed by someone else since you opened it. Reload it, or copy your text first.").
				WithData(map[string]any{"mtime": mt})
		}
	} else if expected != 0 {
		return nil, rpc.Errorf(rpc.Conflict, "This file was removed since you opened it.")
	}
	tmp, terr := os.CreateTemp(filepath.Dir(path), ".save-*")
	if terr == nil {
		tname := tmp.Name()
		_, werr := tmp.Write(data)
		if werr == nil {
			werr = tmp.Sync()
		}
		if cerr := tmp.Close(); werr == nil {
			werr = cerr
		}
		if werr == nil {
			if werr = replaceFile(tname, path); werr == nil {
				return saved(path)
			}
		}
		_ = os.Remove(tname)
		terr = werr
	}
	// The folder may not be writable although the file is: write in place.
	if exists && os.IsPermission(terr) {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
		if err != nil {
			return nil, pathErr("open", path, err)
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, pathErr("write", path, err)
		}
		return saved(path)
	}
	return nil, terr
}
