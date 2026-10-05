//go:build windows

package files

// Windows implementation of the safe file system primitives.
//
// The bridge runs with the signed-in user's token, so the OS enforces ACLs.
// What remains is following reparse points (symlinks, junctions) that someone
// planted to make an operation land elsewhere. The rules:
//   - The final component of every path is never followed: it is inspected
//     with Lstat (isLinkMode) and created with CREATE_NEW semantics (O_EXCL),
//     which fails on an existing link instead of opening its target.
//   - Files are replaced by writing a temp file next to them and renaming it
//     over the name (MoveFileEx), which replaces a link itself, never its target.
//   - Deleting removes links without descending into them.
//   - Copies never follow reparse points (see copier in ops_windows.go).
//
// Intermediate folders are resolved by the OS as usual (a junction such as
// "Documents and Settings" is legitimate); ACLs still apply to the user.

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func errUnsupported() error {
	return rpc.Errorf(rpc.Unavailable, "This operation is not supported on Windows.")
}

func lstatLink(path string) (os.FileInfo, bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, false, err
	}
	return fi, isLinkMode(fi.Mode()), nil
}

// checkExists fails unless something (a link itself, not its target) exists at path.
func checkExists(path string) error {
	_, err := os.Lstat(path)
	return err
}

// mkdirOne creates the single folder path; its parent must exist. perm is ignored (ACLs are inherited).
func mkdirOne(path string, perm uint32) error {
	return pathErr("mkdir", path, os.Mkdir(path, 0o777))
}

// createFile creates the empty regular file path, failing if anything is there.
func createFile(path string, perm uint32) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return pathErr("create", path, err)
	}
	return f.Close()
}

func mkdirAll(path string, perm uint32) error {
	return pathErr("mkdir", path, os.MkdirAll(path, 0o777))
}

// removeOne deletes a file, link or empty folder, clearing the read-only
// attribute when that is what blocks it.
func removeOne(path string) error {
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return err
	}
	p, perr := windows.UTF16PtrFromString(path)
	if perr != nil {
		return err
	}
	if a, aerr := windows.GetFileAttributes(p); aerr == nil && a&windows.FILE_ATTRIBUTE_READONLY != 0 {
		if windows.SetFileAttributes(p, a&^windows.FILE_ATTRIBUTE_READONLY) == nil {
			return os.Remove(path)
		}
	}
	return err
}

// removePath deletes path and everything below it. Links are removed, never followed.
func removePath(path string) error {
	fi, link, err := lstatLink(path)
	if err != nil {
		return pathErr("lstat", path, err)
	}
	if link || !fi.IsDir() {
		return pathErr("remove", path, removeOne(path))
	}
	ents, err := os.ReadDir(path)
	if err != nil {
		return pathErr("readdir", path, err)
	}
	for _, e := range ents {
		if err := removePath(filepath.Join(path, e.Name())); err != nil {
			return err
		}
	}
	return pathErr("remove", path, removeOne(path))
}

// moveNoReplace renames src to dst, failing with Conflict if dst exists. It
// fails across volumes (ERROR_NOT_SAME_DEVICE); callers then copy.
func moveNoReplace(src, dst string) error {
	return moveEx(src, dst, 0)
}

// replaceFile renames the file src over dst (a link at dst is replaced, not followed).
func replaceFile(src, dst string) error {
	return moveEx(src, dst, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func moveEx(src, dst string, flags uint32) error {
	s, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	d, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(s, d, flags)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return rpc.Errorf(rpc.Conflict, "%s already exists.", filepath.Base(dst))
	}
	return pathErr("rename", dst, err)
}

func isCrossVolume(err error) bool { return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) }

// openNoFollow opens a regular file for reading without following a reparse
// point at the final component (checked on the opened handle).
func openNoFollow(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, pathErr("open", path, err)
	}
	var info attrTagInfo
	err = windows.GetFileInformationByHandleEx(h, fileAttributeTagInfoClass, (*byte)(unsafePtr(&info)), uint32(sizeofTagInfo))
	if err != nil {
		windows.CloseHandle(h)
		return nil, pathErr("stat", path, err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 && isNameSurrogate(info.ReparseTag) ||
		info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		windows.CloseHandle(h)
		return nil, errLinkSwapped(path)
	}
	return os.NewFile(uintptr(h), path), nil
}

func errLinkSwapped(path string) error {
	return rpc.Errorf(rpc.Conflict, "%s was replaced by a link or folder while it was being read.", path)
}
