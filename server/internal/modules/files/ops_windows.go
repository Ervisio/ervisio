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
	buf      []byte
	depth    int
	madeTop  bool
	skipped  []string
}

func newCopier(ctx context.Context, progress func(int64)) *copier {
	return &copier{ctx: ctx, progress: progress}
}

func (c *copier) copyAt(src, dst string) error {
	if err := c.ctx.Err(); err != nil {
		return rpc.Errorf(rpc.Unavailable, "The copy was cancelled.")
	}
	c.depth++
	defer func() { c.depth-- }()
	fi, link, err := lstatLink(src)
	if err != nil {
		return pathErr("lstat", src, err)
	}
	switch {
	case link:
		if c.depth == 1 {
			return rpc.Errorf(rpc.Invalid, "%s is a link and cannot be copied. Copy the item it points to instead.", src)
		}
		c.skipped = append(c.skipped, src)
		return nil
	case fi.IsDir():
		if err := os.Mkdir(dst, 0o777); err != nil {
			return pathErr("mkdir", dst, err)
		}
		if c.depth == 1 {
			c.madeTop = true
		}
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
	case fi.Mode().IsRegular():
		return c.copyFile(src, dst, fi)
	}
	c.skipped = append(c.skipped, src) // devices, pipes and the like
	return nil
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
	if c.depth == 1 {
		c.madeTop = true
	}
	fail := func(err error) error {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if c.buf == nil {
		c.buf = make([]byte, 256<<10)
	}
	for {
		if err := c.ctx.Err(); err != nil {
			return fail(rpc.Errorf(rpc.Unavailable, "The copy was cancelled."))
		}
		n, rerr := in.Read(c.buf)
		if n > 0 {
			if _, werr := out.Write(c.buf[:n]); werr != nil {
				return fail(werr)
			}
			if c.progress != nil {
				c.progress(int64(n))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fail(pathErr("read", src, rerr))
		}
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
		return rpc.Errorf(rpc.Conflict, "%s already exists. Choose another name.", to)
	}
	if fi.IsDir() && !link && isInside(to, from) {
		return rpc.Errorf(rpc.Invalid, "A folder cannot be moved into itself.")
	}
	err = moveNoReplace(from, to)
	if err == nil || !isCrossVolume(err) {
		return err
	}
	if link {
		return rpc.Errorf(rpc.Invalid, "%s is a link and cannot be moved to another drive.", from)
	}
	c := newCopier(context.Background(), nil)
	if err := c.copyAt(from, to); err != nil {
		if c.madeTop {
			_ = removePath(to)
		}
		return err
	}
	if len(c.skipped) > 0 { // never delete what was not copied
		_ = removePath(to)
		return rpc.Errorf(rpc.Invalid, "%s contains links or special files and cannot be moved to another drive. Copy it instead.", from)
	}
	return removePath(from)
}

func doCopy(ctx context.Context, p copyParams, s rpc.Stream) error {
	from, err := cleanPaths(p.From)
	if err != nil {
		return err
	}
	to, err := cleanPath(p.To)
	if err != nil {
		return err
	}
	if ti, err := os.Stat(to); err != nil {
		return err
	} else if !ti.IsDir() {
		return rpc.Errorf(rpc.Invalid, "%s is not a folder.", to)
	}
	type job struct{ src, dst string }
	var jobs []job
	var total int64
	for _, src := range from {
		fi, link, err := lstatLink(src)
		if err != nil {
			return err
		}
		if p.Move {
			if err := checkNotProtected(src); err != nil {
				return err
			}
		}
		dst := filepath.Join(to, filepath.Base(src))
		if fi.IsDir() && !link && isInside(to, src) {
			return rpc.Errorf(rpc.Invalid, "A folder cannot be copied or moved into itself.")
		}
		if p.Move && isInside(filepath.Dir(src), to) && isInside(to, filepath.Dir(src)) {
			continue // already there
		}
		jobs = append(jobs, job{src, uniqueName(dst)})
		sz, _ := treeSize(ctx, src)
		total += sz
	}
	if err := s.Send(map[string]any{"type": "start", "total": total, "items": len(jobs)}); err != nil {
		return err
	}
	var done int64
	last := time.Time{}
	cur := ""
	emit := func(force bool) error {
		if force || time.Since(last) > 100*time.Millisecond {
			last = time.Now()
			return s.Send(map[string]any{"type": "progress", "done": done, "total": total, "current": cur})
		}
		return nil
	}
	results := []string{}
	skipped := []string{}
	for _, j := range jobs {
		cur = filepath.Base(j.src)
		if err := emit(true); err != nil {
			return err
		}
		if err := func() error {
			if p.Move {
				err := moveNoReplace(j.src, j.dst)
				if err == nil {
					sz, _ := treeSize(ctx, j.dst)
					done += sz
					return nil
				} else if !isCrossVolume(err) {
					return err
				}
			}
			var perr error
			c := newCopier(ctx, func(n int64) {
				done += n
				if perr == nil {
					perr = emit(false)
				}
			})
			if cerr := c.copyAt(j.src, j.dst); cerr != nil {
				if c.madeTop {
					_ = removePath(j.dst)
				}
				return cerr
			}
			if perr != nil {
				return perr
			}
			skipped = append(skipped, c.skipped...)
			if p.Move {
				if len(c.skipped) > 0 {
					return rpc.Errorf(rpc.Invalid, "%s contains links or special files and cannot be moved to another drive. Copy it instead.", j.src)
				}
				return removePath(j.src)
			}
			return nil
		}(); err != nil {
			return err
		}
		results = append(results, j.dst)
	}
	_ = emit(true)
	msg := map[string]any{"done": true, "paths": results}
	if len(skipped) > 0 {
		msg["skipped"] = skipped // links and special files that were not copied
	}
	return s.Send(msg)
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
	var p struct {
		Path      string `json:"path"`
		Mode      string `json:"mode"`
		Recursive bool   `json:"recursive"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	mode, err := parseMode(p.Mode)
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
		return nil, rpc.Errorf(rpc.Invalid, "Permissions cannot be changed on a link. Change the item it points to instead.")
	}
	if !fi.IsDir() {
		if err := setReadOnly(path, ro); err != nil {
			return nil, pathErr("chmod", path, err)
		}
		return map[string]any{"changed": 1}, nil
	}
	if !p.Recursive {
		return nil, rpc.Errorf(rpc.Invalid, "On Windows the read-only attribute applies to files. Choose \"apply to everything inside\" to change the files in this folder.")
	}
	changed := 0
	var first error
	err = filepath.WalkDir(path, func(q string, d os.DirEntry, werr error) error {
		if ctx.Err() != nil {
			return ctxErr(ctx)
		}
		if werr != nil {
			if first == nil {
				first = werr
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if isLinkMode(d.Type()) || !d.Type().IsRegular() {
			return nil
		}
		if err := setReadOnly(q, ro); err != nil {
			if first == nil {
				first = pathErr("chmod", q, err)
			}
			return nil
		}
		changed++
		return nil
	})
	if err != nil {
		return nil, err
	}
	if first != nil && changed == 0 {
		return nil, first
	}
	res := map[string]any{"changed": changed}
	if first != nil {
		res["warning"] = first.Error()
	}
	return res, nil
}

func hChown(ctx context.Context, c *rpc.Call) (any, error) {
	return nil, rpc.Errorf(rpc.Unavailable, "Ownership on Windows uses ACLs; changing it is not supported here.")
}
