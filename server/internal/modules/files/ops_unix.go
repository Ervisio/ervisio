//go:build unix

package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// movePath renames from to to without ever overwriting. Across file systems it
// copies and then removes the source. Both ends are reached through directory
// descriptors (safefs.go), so a link swapped into either path is never followed.
func movePath(from, to string) error {
	if err := checkNotProtected(from); err != nil {
		return err
	}
	sd, sname, err := openParent(from)
	if err != nil {
		return err
	}
	defer sd.Close()
	st, err := lstatAt(dfd(sd), sname)
	if err != nil {
		return pathErr("lstat", from, err)
	}
	dd, dname, err := openParent(to)
	if err != nil {
		return err
	}
	defer dd.Close()
	if _, err := lstatAt(dfd(dd), dname); err == nil {
		return rpc.Errorf(rpc.Conflict, "%s already exists. Choose another name.", to)
	}
	if isDir(&st) && isInside(to, from) {
		return rpc.Errorf(rpc.Invalid, "A folder cannot be moved into itself.")
	}
	err = renameNoReplace(dfd(sd), sname, dfd(dd), dname, to)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	c := newCopier(context.Background(), nil)
	if err := c.copyAt(dfd(sd), sname, from, dfd(dd), dname, to); err != nil {
		if c.madeTop {
			_ = removeAllAt(dfd(dd), dname, to)
		}
		return err
	}
	return removeAllAt(dfd(sd), sname, from)
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
	toDir, err := openDir(to)
	if err != nil {
		return err
	}
	defer toDir.Close()
	// Plan.
	type job struct{ src, dst string }
	var jobs []job
	var total int64
	for _, src := range from {
		st, err := lstatPath(src)
		if err != nil {
			return err
		}
		if p.Move {
			if err := checkNotProtected(src); err != nil {
				return err
			}
		}
		dst := filepath.Join(to, filepath.Base(src))
		if isDir(&st) && isInside(to, src) {
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
	for _, j := range jobs {
		cur = filepath.Base(j.src)
		if err := emit(true); err != nil {
			return err
		}
		if err := func() error {
			sd, sname, err := openParent(j.src)
			if err != nil {
				return err
			}
			defer sd.Close()
			dname := filepath.Base(j.dst)
			if p.Move {
				err := renameNoReplace(dfd(sd), sname, dfd(toDir), dname, j.dst)
				if err == nil {
					sz, _ := treeSize(ctx, j.dst)
					done += sz
					return nil
				} else if !errors.Is(err, syscall.EXDEV) {
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
			if cerr := c.copyAt(dfd(sd), sname, j.src, dfd(toDir), dname, j.dst); cerr != nil {
				if c.madeTop {
					_ = removeAllAt(dfd(toDir), dname, j.dst)
				}
				return cerr
			}
			if perr != nil {
				return perr
			}
			if p.Move {
				return removeAllAt(dfd(sd), sname, j.src)
			}
			return nil
		}(); err != nil {
			return err
		}
		results = append(results, j.dst)
	}
	_ = emit(true)
	return s.Send(map[string]any{"done": true, "paths": results})
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
	d, name, err := openParent(path)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	st, err := lstatAt(dfd(d), name)
	if err != nil {
		return nil, pathErr("lstat", path, err)
	}
	errLink := rpc.Errorf(rpc.Invalid, "Permissions cannot be changed on a link. Change the item it points to instead.")
	if isLink(&st) {
		return nil, errLink
	}
	m := unixMode(mode)
	if !p.Recursive || !isDir(&st) {
		if err := chmodNoFollow(dfd(d), name, m); err != nil {
			if errors.Is(err, errIsLink) {
				return nil, errLink
			}
			return nil, pathErr("chmod", path, err)
		}
		return map[string]any{"changed": 1}, nil
	}
	changed := 0
	var first error
	err = walkAt(ctx, dfd(d), name, path, func(dirfd int, name string, st *unix.Stat_t, self int) error {
		var err error
		switch {
		case self >= 0:
			err = unix.Fchmod(self, m)
		case isLink(st):
			return nil // links have no permissions of their own
		default:
			if err = chmodNoFollow(dirfd, name, m); errors.Is(err, errIsLink) {
				return nil
			}
		}
		if err == nil {
			changed++
		}
		return err
	}, func(err error) {
		if first == nil {
			first = err
		}
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
	d, name, err := openParent(path)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	st, err := lstatAt(dfd(d), name)
	if err != nil {
		return nil, pathErr("lstat", path, err)
	}
	if !p.Recursive || !isDir(&st) {
		if err := unix.Fchownat(dfd(d), name, uid, gid, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return nil, pathErr("chown", path, err)
		}
		return map[string]any{"changed": 1}, nil
	}
	changed := 0
	var first error
	err = walkAt(ctx, dfd(d), name, path, func(dirfd int, name string, st *unix.Stat_t, self int) error {
		var err error
		if self >= 0 {
			err = unix.Fchown(self, uid, gid)
		} else {
			err = unix.Fchownat(dirfd, name, uid, gid, unix.AT_SYMLINK_NOFOLLOW)
		}
		if err == nil {
			changed++
		}
		return err
	}, func(err error) {
		if first == nil {
			first = err
		}
	})
	if err != nil {
		return nil, err
	}
	if first != nil && changed == 0 {
		return nil, first
	}
	return map[string]any{"changed": changed}, nil
}
