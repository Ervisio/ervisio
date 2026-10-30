package remote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/modules/files"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Entry is the same shape as a local file (docs/api/files.md).
type Entry = files.Entry

// FS is one open connection to a remote file system. Paths are absolute,
// slash-separated and cleaned (path.Clean), "/" being the top of what the
// connection reaches (the server root for SFTP and FTP, the share for SMB,
// the base URL for WebDAV).
//
// Errors use fs.ErrNotExist, fs.ErrExist and fs.ErrPermission where the
// protocol tells them apart, so remoteErr can name them.
type FS interface {
	// List returns the items of a folder (not "." and "..").
	List(ctx context.Context, dir string) ([]Entry, error)
	// Stat describes p without following a final symlink.
	Stat(ctx context.Context, p string) (Entry, error)
	// Mkdir makes one folder; the parent must exist.
	Mkdir(ctx context.Context, p string) error
	// Rename moves from to to. Callers check that to does not exist.
	Rename(ctx context.Context, from, to string) error
	// Remove deletes a file, or an empty folder when dir is set.
	Remove(ctx context.Context, p string, dir bool) error
	// Open reads a file.
	Open(ctx context.Context, p string) (io.ReadCloser, error)
	// Create writes a new file; size is the final size, or -1 when not
	// known. Close commits it; a writer that implements aborter is aborted
	// instead when the data is incomplete.
	Create(ctx context.Context, p string, size int64) (io.WriteCloser, error)
	// Close hangs up.
	Close() error
}

// removeAller is implemented by protocols that delete a tree in one request.
type removeAller interface {
	RemoveAll(ctx context.Context, p string) error
}

// aborter is implemented by writers that must not commit partial data.
type aborter interface {
	Abort(err error)
}

// maxList bounds one folder listing, like files.list.
const maxList = 50000

var mimeOf = files.MimeOf

// cleanRemote validates a remote path.
func cleanRemote(p string) (string, error) {
	if p == "" {
		return "", rpc.Errorf(rpc.Invalid, "A path is missing.")
	}
	if len(p) > 4096 || strings.ContainsRune(p, 0) {
		return "", rpc.Errorf(rpc.Invalid, "The path is not valid.")
	}
	if !strings.HasPrefix(p, "/") {
		return "", rpc.Errorf(rpc.Invalid, "The path %q must start with /.", p)
	}
	return path.Clean(p), nil
}

func validName(n string) error {
	if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\\\x00") {
		return rpc.Errorf(rpc.Invalid, "%q is not a valid file name.", n)
	}
	return nil
}

// infoEntry builds an Entry from an os.FileInfo. Owner fields stay empty:
// remote accounts cannot be named from here.
func infoEntry(dir string, fi os.FileInfo) Entry {
	e := Entry{
		Name:  fi.Name(),
		Path:  path.Join(dir, fi.Name()),
		Type:  files.TypeOf(fi.Mode()),
		Size:  fi.Size(),
		Mode:  files.ModeOctal(fi.Mode()),
		Perm:  files.ModeString(fi.Mode()),
		MTime: fi.ModTime().UnixMilli(),
	}
	if e.Type == "dir" {
		e.Size = 0
	}
	if e.Type == "file" {
		e.Mime = files.MimeOf(e.Name)
	}
	return e
}

// dirEntry is the entry of a folder the protocol does not describe (a root).
func dirEntry(p string) Entry {
	n := path.Base(p)
	return Entry{Name: n, Path: p, Type: "dir", Mode: "0755", Perm: "rwxr-xr-x"}
}

func setIDs(e *Entry, uid, gid uint32) {
	e.UID, e.GID = int(uid), int(gid)
	e.Owner, e.Group = strconv.FormatUint(uint64(uid), 10), strconv.FormatUint(uint64(gid), 10)
}

func isDirLike(e Entry) bool {
	return e.Type == "dir" || (e.Type == "symlink" && e.TargetType == "dir")
}

func isFileLike(e Entry) bool {
	return e.Type == "file" || (e.Type == "symlink" && e.TargetType == "file")
}

func exists(ctx context.Context, f FS, p string) (bool, error) {
	_, err := f.Stat(ctx, p)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// removeAll deletes p and everything below it. Symlinks are removed, never
// followed.
func removeAll(ctx context.Context, f FS, p string) error {
	if p == "/" {
		return rpc.Errorf(rpc.Forbidden, "The top folder of a connection cannot be deleted.")
	}
	if ra, ok := f.(removeAller); ok {
		return ra.RemoveAll(ctx, p)
	}
	e, err := f.Stat(ctx, p)
	if err != nil {
		return err
	}
	return removeTree(ctx, f, p, e, 0)
}

func removeTree(ctx context.Context, f FS, p string, e Entry, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.Type != "dir" {
		return f.Remove(ctx, p, false)
	}
	if depth > 256 {
		return rpc.Errorf(rpc.Invalid, "%s is nested too deeply.", p)
	}
	kids, err := f.List(ctx, p)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if err := removeTree(ctx, f, path.Join(p, k.Name), k, depth+1); err != nil {
			return err
		}
	}
	return f.Remove(ctx, p, true)
}

func randName(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b) + ".tmp"
}

// pathOps are the path functions of one side: slash paths for remote
// connections, native ones for this computer.
type pathOps struct {
	join func(elem ...string) string
	dir  func(string) string
	base func(string) string
}

var (
	posixOps  = pathOps{path.Join, path.Dir, path.Base}
	nativeOps = pathOps{filepath.Join, filepath.Dir, filepath.Base}
)

// writeFile stores r as p: the data goes to a hidden temporary file next to
// p, which is renamed once complete, so a cut connection never leaves a
// half-written p. Without overwrite an existing p is a conflict.
func writeFile(ctx context.Context, f FS, ops pathOps, p string, r io.Reader, size int64, overwrite bool, progress func(int64)) (int64, error) {
	if ops.dir(p) == p {
		return 0, rpc.Errorf(rpc.Invalid, "That is not a valid target file name.")
	}
	if err := validName(ops.base(p)); err != nil {
		return 0, err
	}
	name := ops.base(p)
	if e, err := f.Stat(ctx, p); err == nil {
		if !overwrite {
			return 0, rpc.Errorf(rpc.Conflict, "%s already exists.", name)
		}
		if isDirLike(e) {
			return 0, rpc.Errorf(rpc.Invalid, "%s is a folder and cannot be replaced by a file.", name)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	tmp := ops.join(ops.dir(p), randName(".ervisio-upload-"))
	w, err := f.Create(ctx, tmp, size)
	if err != nil {
		return 0, err
	}
	n, err := copyCtx(ctx, w, r, progress)
	if err == nil && size >= 0 && n != size {
		err = rpc.Errorf(rpc.Invalid, "The data ended early (%d of %d bytes). Try again.", n, size)
	}
	if err != nil {
		if a, ok := w.(aborter); ok {
			a.Abort(err)
		} else {
			_ = w.Close()
		}
		_ = f.Remove(context.WithoutCancel(ctx), tmp, false)
		return n, err
	}
	if err := w.Close(); err != nil {
		_ = f.Remove(context.WithoutCancel(ctx), tmp, false)
		return n, err
	}
	if overwrite {
		if ok, _ := exists(ctx, f, p); ok {
			if err := f.Remove(ctx, p, false); err != nil {
				_ = f.Remove(context.WithoutCancel(ctx), tmp, false)
				return n, err
			}
		}
	} else if ok, _ := exists(ctx, f, p); ok {
		// It appeared while the data was being sent.
		_ = f.Remove(context.WithoutCancel(ctx), tmp, false)
		return n, rpc.Errorf(rpc.Conflict, "%s already exists.", name)
	}
	if err := f.Rename(ctx, tmp, p); err != nil {
		_ = f.Remove(context.WithoutCancel(ctx), tmp, false)
		return n, err
	}
	return n, nil
}

// copyCtx copies in 64 KiB steps, stopping when ctx ends.
func copyCtx(ctx context.Context, w io.Writer, r io.Reader, progress func(int64)) (int64, error) {
	buf := make([]byte, 64<<10)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		k, rerr := r.Read(buf)
		if k > 0 {
			wk, werr := w.Write(buf[:k])
			n += int64(wk)
			if progress != nil {
				progress(int64(wk))
			}
			if werr != nil {
				return n, werr
			}
			if wk != k {
				return n, io.ErrShortWrite
			}
		}
		if rerr == io.EOF {
			return n, nil
		}
		if rerr != nil {
			return n, rerr
		}
	}
}

// pipeWriter turns a function that consumes a reader (an FTP STOR, an HTTP
// PUT) into an io.WriteCloser.
type pipeWriter struct {
	pw   *io.PipeWriter
	done chan error
}

func newPipeWriter(consume func(r io.Reader) error) *pipeWriter {
	pr, pw := io.Pipe()
	w := &pipeWriter{pw: pw, done: make(chan error, 1)}
	go func() {
		err := consume(pr)
		_ = pr.CloseWithError(errors.New("upload ended"))
		w.done <- err
	}()
	return w
}

func (w *pipeWriter) Write(b []byte) (int, error) {
	n, err := w.pw.Write(b)
	if err != nil {
		// The consumer stopped: report its error rather than the pipe's.
		select {
		case cerr := <-w.done:
			w.done <- cerr
			if cerr != nil {
				return n, cerr
			}
		default:
		}
	}
	return n, err
}

func (w *pipeWriter) Close() error {
	_ = w.pw.Close()
	return <-w.done
}

func (w *pipeWriter) Abort(err error) {
	_ = w.pw.CloseWithError(err)
	select {
	case <-w.done:
	case <-time.After(30 * time.Second):
	}
}

// uniqueName returns p, or "name (copy).ext", "name (copy 2).ext"… when p
// exists (the same rule as local copies).
func uniqueName(ctx context.Context, f FS, ops pathOps, p string) (string, error) {
	ok, err := exists(ctx, f, p)
	if err != nil || !ok {
		return p, err
	}
	dir, base := ops.dir(p), ops.base(p)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem, ext = base, ""
	}
	for i := 1; i < 1000; i++ {
		tag := " (copy)"
		if i > 1 {
			tag = fmt.Sprintf(" (copy %d)", i)
		}
		c := ops.join(dir, stem+tag+ext)
		ok, err := exists(ctx, f, c)
		if err != nil {
			return "", err
		}
		if !ok {
			return c, nil
		}
	}
	return "", rpc.Errorf(rpc.Conflict, "%s already exists.", base)
}
