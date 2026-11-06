package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ervisio/ervisio/server/internal/modules/remote"
)

// The selected folder remains untouched. All writes/deletions are confined to
// a private, provenance-marked subdirectory owned by this plan.
type localBackupFS struct {
	root      *os.Root
	namespace string
}

func openLocalDestination(c *Config) (*localBackupFS, *Config, error) {
	resolved, err := filepath.EvalSymlinks(c.Destination.Path)
	if err != nil {
		return nil, nil, errors.New("The local destination folder is unavailable to this account.")
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, nil, errors.New("The local destination folder cannot be opened by this account.")
	}
	clone := *c
	clone.Destination.Path = "/"
	sum := sha256.Sum256([]byte("local-backup\n" + resolved))
	clone.Destination.Connection = hex.EncodeToString(sum[:8]) // binds recovery to the selected root
	return &localBackupFS{root: root, namespace: "ervisio-backup-" + c.ID}, &clone, nil
}

func (f *localBackupFS) relative(p string) (string, error) {
	if p == "/" {
		return ".", nil
	}
	if p != path.Clean(p) || strings.ContainsAny(p, "\\\x00") || p != "/"+f.namespace && path.Dir(p) != "/"+f.namespace {
		return "", errors.New("The local backup path is outside this plan's namespace.")
	}
	return strings.TrimPrefix(p, "/"), nil
}

func (f *localBackupFS) checkNamespace() error {
	fi, err := f.root.Lstat(f.namespace)
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || !privateOwner(fi) {
		return errors.New("The local backup namespace must be a private directory owned by this account.")
	}
	return nil
}

func (f *localBackupFS) Stat(ctx context.Context, p string) (remote.Entry, error) {
	if err := ctx.Err(); err != nil {
		return remote.Entry{}, err
	}
	r, err := f.relative(p)
	if err != nil {
		return remote.Entry{}, err
	}
	if p != "/"+f.namespace && p != "/" {
		if err := f.checkNamespace(); err != nil {
			return remote.Entry{}, err
		}
	}
	fi, err := f.root.Lstat(r)
	if err != nil {
		return remote.Entry{}, err
	}
	kind := "file"
	if fi.IsDir() {
		kind = "dir"
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		kind = "symlink"
	}
	if p == "/"+f.namespace && (kind != "dir" || !privateOwner(fi)) {
		return remote.Entry{}, errors.New("The existing local backup namespace is not a private account-owned folder.")
	}
	return remote.Entry{Name: path.Base(p), Path: p, Type: kind, Size: fi.Size(), MTime: fi.ModTime().UnixMilli()}, nil
}

func (f *localBackupFS) Mkdir(ctx context.Context, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := f.relative(p)
	if err != nil {
		return err
	}
	if p != "/"+f.namespace {
		return errors.New("Backup can create only its own private namespace.")
	}
	return f.root.Mkdir(r, 0700)
}

func (f *localBackupFS) List(ctx context.Context, p string) ([]remote.Entry, error) {
	r, err := f.relative(p)
	if err != nil {
		return nil, err
	}
	if err := f.checkNamespace(); err != nil {
		return nil, err
	}
	dir, err := openArchiveRead(f.root, r, true)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(5001)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 5000 {
		return nil, errors.New("Too many local destination entries for safe retention.")
	}
	items := make([]remote.Entry, 0, len(entries))
	for _, entry := range entries {
		value, err := f.Stat(ctx, path.Join(p, entry.Name()))
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, nil
}

func (f *localBackupFS) Open(ctx context.Context, p string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := f.relative(p)
	if err != nil {
		return nil, err
	}
	if err := f.checkNamespace(); err != nil {
		return nil, err
	}
	return openArchiveRead(f.root, r, false)
}

type localBackupWriter struct{ *os.File }

func (w *localBackupWriter) Close() error {
	err := w.Sync()
	closeErr := w.File.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (f *localBackupFS) Create(ctx context.Context, p string, _ int64) (io.WriteCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := f.relative(p)
	if err != nil {
		return nil, err
	}
	if path.Dir(p) != "/"+f.namespace {
		return nil, errors.New("Cannot replace the destination root.")
	}
	if err := f.checkNamespace(); err != nil {
		return nil, err
	}
	file, err := f.root.OpenFile(r, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	return &localBackupWriter{file}, nil
}

func (f *localBackupFS) Rename(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := f.relative(from)
	if err != nil {
		return err
	}
	target, err := f.relative(to)
	if err != nil {
		return err
	}
	if path.Dir(from) != "/"+f.namespace || path.Dir(to) != "/"+f.namespace {
		return errors.New("Cannot move the destination namespace.")
	}
	if err := f.checkNamespace(); err != nil {
		return err
	}
	fi, err := f.root.Lstat(source)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("Only regular backup files can be committed.")
	}
	// Link publishes atomically without replacing an existing name. Do not
	// weaken this to an overwriting rename on filesystems without hard links.
	if err := f.root.Link(source, target); err != nil {
		return errors.New("The local filesystem could not commit a backup without overwriting an existing file. Check hard-link support and available space.")
	}
	return f.root.Remove(source)
}

func (f *localBackupFS) Remove(ctx context.Context, p string, directory bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := f.relative(p)
	if err != nil {
		return err
	}
	if directory || path.Dir(p) != "/"+f.namespace {
		return errors.New("The destination folder cannot be removed by retention.")
	}
	if err := f.checkNamespace(); err != nil {
		return err
	}
	return f.root.Remove(r)
}
func (f *localBackupFS) Close() error { return f.root.Close() }
