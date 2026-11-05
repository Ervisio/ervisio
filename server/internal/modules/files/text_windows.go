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
	exists, err := checkSaveTarget(path, expected)
	if err != nil {
		return nil, err
	}
	terr := saveViaTemp(path, data)
	if terr == nil {
		return saved(path)
	}
	// The folder may not be writable although the file is: write in place.
	if exists && os.IsPermission(terr) {
		return saveInPlace(path, data)
	}
	return nil, terr
}

// checkSaveTarget reports whether path exists and checks it is still what the
// editor opened.
func checkSaveTarget(path string, expected int64) (bool, error) {
	fi, link, err := lstatLink(path)
	if os.IsNotExist(err) {
		return false, checkExpectedAbsent(expected)
	}
	if err != nil {
		return false, pathErr("lstat", path, err)
	}
	if link {
		return true, rpc.Errorf(rpc.Invalid, "%s is a link. Open the file it points to instead.", filepath.Base(path))
	}
	if !fi.Mode().IsRegular() {
		return true, errNotRegularText()
	}
	return true, checkExpectedMtime(fi.ModTime().UnixMilli(), expected)
}

func saveViaTemp(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".save-*")
	if err != nil {
		return err
	}
	tname := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		if err = replaceFile(tname, path); err == nil {
			return nil
		}
	}
	_ = os.Remove(tname)
	return err
}

func saveInPlace(path string, data []byte) (any, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return nil, pathErr("open", path, err)
	}
	return finishInPlaceSave(f, path, data, nil)
}
