package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// isInside reports whether child is parent or lies below it.
func isInside(child, parent string) bool {
	if parent == "/" {
		return true
	}
	return child == parent || strings.HasPrefix(child, parent+"/")
}

// movePath renames from to to without ever overwriting. Across file systems it
// copies and then removes the source.
func movePath(from, to string) error {
	if err := checkNotProtected(from); err != nil {
		return err
	}
	fi, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(to); err == nil {
		return rpc.Errorf(rpc.Conflict, "%s already exists. Choose another name.", to)
	}
	if fi.IsDir() && isInside(to, from) {
		return rpc.Errorf(rpc.Invalid, "A folder cannot be moved into itself.")
	}
	err = os.Rename(from, to)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyTree(context.Background(), from, to, nil); err != nil {
		_ = os.RemoveAll(to)
		return err
	}
	return os.RemoveAll(from)
}

// copyTree copies src to dst (which must not exist), without following symlinks.
func copyTree(ctx context.Context, src, dst string, progress func(n int64)) error {
	if err := ctx.Err(); err != nil {
		return rpc.Errorf(rpc.Unavailable, "The copy was cancelled.")
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		t, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(t, dst)
	case fi.IsDir():
		if err := os.Mkdir(dst, fi.Mode().Perm()|0o700); err != nil {
			return err
		}
		des, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, de := range des {
			if err := copyTree(ctx, filepath.Join(src, de.Name()), filepath.Join(dst, de.Name()), progress); err != nil {
				return err
			}
		}
		if err := os.Chmod(dst, fi.Mode().Perm()); err != nil {
			return err
		}
		return os.Chtimes(dst, time.Now(), fi.ModTime())
	case fi.Mode().IsRegular():
		return copyFile(ctx, src, dst, fi, progress)
	}
	return nil // sockets, devices and pipes are skipped
}

func copyFile(ctx context.Context, src, dst string, fi os.FileInfo, progress func(n int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	buf := make([]byte, 256<<10)
	for {
		if err := ctx.Err(); err != nil {
			out.Close()
			os.Remove(dst)
			return rpc.Errorf(rpc.Unavailable, "The copy was cancelled.")
		}
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(dst)
				return werr
			}
			if progress != nil {
				progress(int64(n))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			os.Remove(dst)
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	_ = os.Chmod(dst, fi.Mode().Perm())
	return os.Chtimes(dst, time.Now(), fi.ModTime())
}

// treeSize sums regular file sizes below p (symlinks are not followed).
func treeSize(ctx context.Context, p string) (size int64, items int) {
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		items++
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				size += fi.Size()
			}
		}
		return nil
	})
	return
}

// uniqueName returns p, or "name (copy).ext", "name (copy 2).ext"... if p exists.
func uniqueName(p string) string {
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	dir, base := filepath.Split(p)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" { // dot files such as ".bashrc"
		stem, ext = base, ""
	}
	for i := 1; i < 10000; i++ {
		tag := " (copy)"
		if i > 1 {
			tag = fmt.Sprintf(" (copy %d)", i)
		}
		c := filepath.Join(dir, stem+tag+ext)
		if _, err := os.Lstat(c); err != nil {
			return c
		}
	}
	return p
}

type copyParams struct {
	From []string `json:"from"`
	To   string   `json:"to"`
	Move bool     `json:"move"`
}

func hCopy(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p copyParams
	if err := c.Bind(&p); err != nil {
		return err
	}
	return doCopy(ctx, p, s)
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
	// Plan.
	type job struct{ src, dst string }
	var jobs []job
	var total int64
	for _, src := range from {
		fi, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if p.Move {
			if err := checkNotProtected(src); err != nil {
				return err
			}
		}
		dst := filepath.Join(to, filepath.Base(src))
		if fi.IsDir() && isInside(to, src) {
			return rpc.Errorf(rpc.Invalid, "A folder cannot be copied or moved into itself.")
		}
		if p.Move && filepath.Dir(src) == to {
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
	var results []string
	for i, j := range jobs {
		cur = filepath.Base(j.src)
		if err := emit(true); err != nil {
			return err
		}
		if p.Move {
			if err := os.Rename(j.src, j.dst); err == nil {
				sz, _ := treeSize(ctx, j.dst)
				done += sz
				results = append(results, j.dst)
				continue
			} else if !errors.Is(err, syscall.EXDEV) {
				return err
			}
		}
		var perr error
		cerr := copyTree(ctx, j.src, j.dst, func(n int64) {
			done += n
			if perr == nil {
				perr = emit(false)
			}
		})
		if cerr != nil {
			_ = os.RemoveAll(j.dst)
			return cerr
		}
		if perr != nil {
			return perr
		}
		if p.Move {
			if err := os.RemoveAll(j.src); err != nil {
				return err
			}
		}
		results = append(results, j.dst)
		_ = i
	}
	_ = emit(true)
	return s.Send(map[string]any{"done": true, "paths": results})
}

// ---- delete / trash ----

func hDelete(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Paths []string `json:"paths"`
		Trash bool     `json:"trash"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	paths, err := cleanPaths(p.Paths)
	if err != nil {
		return nil, err
	}
	return doDelete(paths, p.Trash)
}

type failure struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

func doDelete(paths []string, trash bool) (any, error) {
	home, _ := homeDir()
	deleted := []string{}
	failed := []failure{}
	var firstErr error
	for _, p := range paths {
		err := checkNotProtected(p)
		if err == nil && home != "" && p == home {
			err = rpc.Errorf(rpc.Forbidden, "Your home folder cannot be deleted from here.")
		}
		if err == nil {
			if _, e := os.Lstat(p); e != nil {
				err = e
			}
		}
		if err == nil {
			if trash {
				err = moveToTrash(p)
			} else {
				err = os.RemoveAll(p) // never follows symlinks
			}
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			re := rpc.ToError(err, false)
			failed = append(failed, failure{Path: p, Message: re.Message, Code: string(re.Code)})
			continue
		}
		deleted = append(deleted, p)
	}
	if len(deleted) == 0 && firstErr != nil {
		return nil, firstErr // lets needs_admin reach the client
	}
	return map[string]any{"deleted": deleted, "failed": failed}, nil
}

// homeOverride lets tests pick the home directory.
var homeOverride string

func homeDir() (string, error) {
	if homeOverride != "" {
		return homeOverride, nil
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return filepath.Clean(u.HomeDir), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Clean(h), nil
}

// ---- chmod / chown ----

func parseMode(s string) (os.FileMode, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 4 {
		return 0, rpc.Errorf(rpc.Invalid, "The permissions %q are not valid. Use an octal number such as 644 or 0755.", s)
	}
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, rpc.Errorf(rpc.Invalid, "The permissions %q are not valid. Use an octal number such as 644 or 0755.", s)
	}
	m := os.FileMode(v & 0o777)
	if v&0o4000 != 0 {
		m |= os.ModeSetuid
	}
	if v&0o2000 != 0 {
		m |= os.ModeSetgid
	}
	if v&0o1000 != 0 {
		m |= os.ModeSticky
	}
	return m, nil
}

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
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, rpc.Errorf(rpc.Invalid, "Permissions cannot be changed on a link. Change the item it points to instead.")
	}
	changed := 0
	if !p.Recursive || !fi.IsDir() {
		if err := os.Chmod(path, mode); err != nil {
			return nil, err
		}
		return map[string]any{"changed": 1}, nil
	}
	var first error
	err = filepath.WalkDir(path, func(q string, d fs.DirEntry, werr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if werr != nil {
			if first == nil {
				first = werr
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if err := os.Chmod(q, mode); err != nil {
			if first == nil {
				first = err
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

func resolveID(name string, group bool) (int, error) {
	if name == "" {
		return -1, nil
	}
	if n, err := strconv.Atoi(name); err == nil && n >= 0 {
		return n, nil
	}
	if group {
		g, err := user.LookupGroup(name)
		if err != nil {
			return 0, rpc.Errorf(rpc.NotFound, "There is no group called %q.", name)
		}
		return strconv.Atoi(g.Gid)
	}
	u, err := user.Lookup(name)
	if err != nil {
		return 0, rpc.Errorf(rpc.NotFound, "There is no user called %q.", name)
	}
	return strconv.Atoi(u.Uid)
}

func hChown(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Path      string `json:"path"`
		Owner     string `json:"owner"`
		Group     string `json:"group"`
		Recursive bool   `json:"recursive"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	uid, err := resolveID(p.Owner, false)
	if err != nil {
		return nil, err
	}
	gid, err := resolveID(p.Group, true)
	if err != nil {
		return nil, err
	}
	if uid == -1 && gid == -1 {
		return nil, rpc.Errorf(rpc.Invalid, "Choose a new owner or group.")
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	changed := 0
	if !p.Recursive || !fi.IsDir() {
		if err := os.Lchown(path, uid, gid); err != nil {
			return nil, err
		}
		return map[string]any{"changed": 1}, nil
	}
	var first error
	err = filepath.WalkDir(path, func(q string, d fs.DirEntry, werr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if werr != nil {
			if first == nil {
				first = werr
			}
			return nil
		}
		if err := os.Lchown(q, uid, gid); err != nil {
			if first == nil {
				first = err
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
	return map[string]any{"changed": changed}, nil
}
