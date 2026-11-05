package remote

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ervisio/ervisio/server/internal/modules/files"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// localFS is this computer's file system, used as one end of remote.copy.
// It runs with the rights of the signed-in account (the user bridge); paths
// are native (C:\… on Windows). New files are created exclusively, so a
// link planted at a target name is never written through.
type localFS struct{}

func cleanLocal(p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) || len(p) > 4096 {
		return "", rpc.Errorf(rpc.Invalid, "The path is not valid.")
	}
	if !filepath.IsAbs(p) {
		return "", rpc.Errorf(rpc.Invalid, "The path %q must be absolute.", p)
	}
	return filepath.Clean(p), nil
}

func localEntry(p string, fi os.FileInfo) Entry {
	e := Entry{Name: fi.Name(), Path: p, Type: files.TypeOf(fi.Mode()), Size: fi.Size(), Mode: files.ModeOctal(fi.Mode()), Perm: files.ModeString(fi.Mode()), MTime: fi.ModTime().UnixMilli()}
	if e.Type == "symlink" {
		if ti, err := os.Stat(p); err == nil {
			e.TargetType = files.TypeOf(ti.Mode())
		}
	}
	return e
}

func (localFS) List(ctx context.Context, dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(des))
	for _, d := range des {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fi, err := d.Info()
		if err != nil {
			continue
		}
		out = append(out, localEntry(filepath.Join(dir, d.Name()), fi))
	}
	return out, nil
}

func (localFS) Stat(ctx context.Context, p string) (Entry, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return Entry{}, err
	}
	return localEntry(p, fi), nil
}

func (localFS) Mkdir(ctx context.Context, p string) error { return os.Mkdir(p, 0o755) }

func (localFS) Rename(ctx context.Context, from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return os.ErrExist
	}
	return os.Rename(from, to)
}

func (localFS) Remove(ctx context.Context, p string, dir bool) error { return os.Remove(p) }

func (localFS) Open(ctx context.Context, p string) (io.ReadCloser, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, rpc.Errorf(rpc.Invalid, "%s is not a regular file.", p)
	}
	return f, nil
}

func (localFS) Create(ctx context.Context, p string, size int64) (io.WriteCloser, error) {
	return os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
}

func (localFS) Close() error { return nil }

// RemoveAll refuses system folders like a local move does.
func (localFS) RemoveAll(ctx context.Context, p string) error {
	if err := files.CheckNotProtected(p); err != nil {
		return err
	}
	if _, err := os.Lstat(p); err != nil {
		return err
	}
	err := os.RemoveAll(p)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
