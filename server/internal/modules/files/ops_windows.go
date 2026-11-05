//go:build windows

package files

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// copier copies trees without ever following reparse points. Policy: a link
// found inside a copied tree is SKIPPED (recreating links needs privileges and
// could point outside the copy) and reported in the "skipped" result list; a
// link given as the copy source itself is refused.
type copier struct {
	ctx      context.Context
	progress func(int64)
	copyBuffer
	depth   int
	madeTop bool
	skipped []string
}

func newCopier(ctx context.Context, progress func(int64)) *copier {
	return &copier{ctx: ctx, progress: progress}
}

func (c *copier) created() {
	if c.depth == 1 {
		c.madeTop = true
	}
}

func (c *copier) copyAt(src, dst string) error {
	if err := c.ctx.Err(); err != nil {
		return errCopyCancelled()
	}
	c.depth++
	defer func() { c.depth-- }()
	fi, link, err := lstatLink(src)
	if err != nil {
		return pathErr("lstat", src, err)
	}
	switch {
	case link:
		return c.skipLink(src)
	case fi.IsDir():
		return c.copyDir(src, dst, fi)
	case fi.Mode().IsRegular():
		return c.copyFile(src, dst, fi)
	}
	c.skipped = append(c.skipped, src) // devices, pipes and the like
	return nil
}

// skipLink records a link met inside a copied tree; as the copy source itself
// it is refused.
func (c *copier) skipLink(src string) error {
	if c.depth == 1 {
		return rpc.Errorf(rpc.Invalid, "%s is a link and cannot be copied. Copy the item it points to instead.", src)
	}
	c.skipped = append(c.skipped, src)
	return nil
}

func (c *copier) copyDir(src, dst string, fi os.FileInfo) error {
	if err := os.Mkdir(dst, 0o777); err != nil {
		return pathErr("mkdir", dst, err)
	}
	c.created()
	ents, err := os.ReadDir(src)
	if err != nil {
		return pathErr("readdir", src, err)
	}
	for _, e := range ents {
		if err := c.copyAt(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return pathErr("chtimes", dst, os.Chtimes(dst, time.Now(), fi.ModTime()))
}

func (c *copier) copyFile(src, dst string, fi os.FileInfo) error {
	in, err := openNoFollow(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return pathErr("create", dst, err)
	}
	c.created()
	if err := c.pump(in, out, src); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	_ = os.Chtimes(dst, time.Now(), fi.ModTime())
	if fi.Mode().Perm()&0o200 == 0 { // keep the read-only attribute
		_ = setReadOnly(dst, true)
	}
	return nil
}

// pump copies everything readable from in into out, reporting progress.
func (c *copier) pump(in io.Reader, out io.Writer, src string) error {
	buf := c.buffer()
	for {
		if err := c.ctx.Err(); err != nil {
			return errCopyCancelled()
		}
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			if c.progress != nil {
				c.progress(int64(n))
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return pathErr("read", src, rerr)
		}
	}
}

func errMoveSkipped(path string) error {
	return rpc.Errorf(rpc.Invalid, "%s contains links or special files and cannot be moved to another drive. Copy it instead.", path)
}

// movePath renames from to to without ever overwriting. Across volumes it
// copies and then removes the source.
func movePath(from, to string) error {
	if err := checkNotProtected(from); err != nil {
		return err
	}
	fi, link, err := lstatLink(from)
	if err != nil {
		return pathErr("lstat", from, err)
	}
	if _, err := os.Lstat(to); err == nil {
		return errAlreadyExists(to)
	}
	if fi.IsDir() && !link && isInside(to, from) {
		return errMoveIntoItself()
	}
	err = moveNoReplace(from, to)
	if err == nil || !isCrossVolume(err) {
		return err
	}
	if link {
		return rpc.Errorf(rpc.Invalid, "%s is a link and cannot be moved to another drive.", from)
	}
	return moveAcrossVolumes(from, to)
}

// moveAcrossVolumes copies from to to and then removes from, undoing the copy
// when it fails.
func moveAcrossVolumes(from, to string) error {
	c := newCopier(context.Background(), nil)
	if err := c.copyAt(from, to); err != nil {
		if c.madeTop {
			_ = removePath(to)
		}
		return err
	}
	if len(c.skipped) > 0 { // never delete what was not copied
		_ = removePath(to)
		return errMoveSkipped(from)
	}
	return removePath(from)
}

func doCopy(ctx context.Context, p copyParams, s rpc.Stream) error {
	from, to, err := copyTargets(p)
	if err != nil {
		return err
	}
	jobs, total, err := planCopy(ctx, p, from, to)
	if err != nil {
		return err
	}
	r := &copyReporter{s: s, total: total}
	if err := r.start(len(jobs)); err != nil {
		return err
	}
	var skipped []string
	results, err := r.run(jobs, func(j copyJob) error {
		return copyJobWin(ctx, p.Move, j, r, &skipped)
	})
	if err != nil {
		return err
	}
	if results == nil {
		results = []string{} // the answer lists paths even when there are none
	}
	msg := map[string]any{"done": true, "paths": results}
	if len(skipped) > 0 {
		msg["skipped"] = skipped // links and special files that were not copied
	}
	return r.finish(msg)
}

// planCopy checks every source and picks its free destination name inside to.
func planCopy(ctx context.Context, p copyParams, from []string, to string) ([]copyJob, int64, error) {
	var jobs []copyJob
	var total int64
	for _, src := range from {
		fi, link, err := lstatLink(src)
		if err != nil {
			return nil, 0, err
		}
		if p.Move {
			if err := checkNotProtected(src); err != nil {
				return nil, 0, err
			}
		}
		if fi.IsDir() && !link && isInside(to, src) {
			return nil, 0, errCopyIntoItself()
		}
		if p.Move && isInside(filepath.Dir(src), to) && isInside(to, filepath.Dir(src)) {
			continue // already there
		}
		jobs = append(jobs, copyJob{src, uniqueName(filepath.Join(to, filepath.Base(src)))})
		sz, _ := treeSize(ctx, src)
		total += sz
	}
	return jobs, total, nil
}

// copyJobWin copies (or moves) one job; links and special files left out of
// the copy are added to skipped.
func copyJobWin(ctx context.Context, move bool, j copyJob, r *copyReporter, skipped *[]string) error {
	if move {
		err := moveNoReplace(j.src, j.dst)
		if err == nil {
			sz, _ := treeSize(ctx, j.dst)
			r.done += sz
			return nil
		}
		if !isCrossVolume(err) {
			return err
		}
	}
	c := newCopier(ctx, r.onBytes)
	if err := c.copyAt(j.src, j.dst); err != nil {
		if c.madeTop {
			_ = removePath(j.dst)
		}
		return err
	}
	if r.err != nil {
		return r.err
	}
	*skipped = append(*skipped, c.skipped...)
	if !move {
		return nil
	}
	if len(c.skipped) > 0 {
		return errMoveSkipped(j.src)
	}
	return removePath(j.src)
}

func setReadOnly(path string, ro bool) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	a, err := windows.GetFileAttributes(p)
	if err != nil {
		return err
	}
	if ro {
		a |= windows.FILE_ATTRIBUTE_READONLY
	} else {
		a &^= windows.FILE_ATTRIBUTE_READONLY
	}
	if a == 0 {
		a = windows.FILE_ATTRIBUTE_NORMAL
	}
	return windows.SetFileAttributes(p, a)
}

// hChmod maps the mode to the read-only attribute (see winReadOnlyFromMode).
// Folders are only changed recursively, and then only the files inside; links
// are never touched or followed.
func hChmod(ctx context.Context, c *rpc.Call) (any, error) {
	path, mode, recursive, err := bindChmod(c)
	if err != nil {
		return nil, err
	}
	ro, err := winReadOnlyFromMode(mode)
	if err != nil {
		return nil, err
	}
	fi, link, err := lstatLink(path)
	if err != nil {
		return nil, pathErr("lstat", path, err)
	}
	if link {
		return nil, errChmodOnLink()
	}
	if !fi.IsDir() {
		if err := setReadOnly(path, ro); err != nil {
			return nil, pathErr("chmod", path, err)
		}
		return map[string]any{"changed": 1}, nil
	}
	if !recursive {
		return nil, rpc.Errorf(rpc.Invalid, "On Windows the read-only attribute applies to files. Choose \"apply to everything inside\" to change the files in this folder.")
	}
	return setReadOnlyTree(ctx, path, ro)
}

// setReadOnlyTree sets the read-only attribute of every regular file below path.
func setReadOnlyTree(ctx context.Context, path string, ro bool) (any, error) {
	changed := 0
	var first firstErr
	err := filepath.WalkDir(path, func(q string, d os.DirEntry, werr error) error {
		if ctx.Err() != nil {
			return ctxErr(ctx)
		}
		if werr != nil {
			first.note(werr)
			return nil
		}
		if d.IsDir() || isLinkMode(d.Type()) || !d.Type().IsRegular() {
			return nil
		}
		if err := setReadOnly(q, ro); err != nil {
			first.note(pathErr("chmod", q, err))
			return nil
		}
		changed++
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changeResult(changed, first.err)
}

func hChown(ctx context.Context, c *rpc.Call) (any, error) {
	return nil, rpc.Errorf(rpc.Unavailable, "Ownership on Windows uses ACLs; changing it is not supported here.")
}
