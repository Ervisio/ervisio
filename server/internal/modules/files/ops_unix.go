//go:build unix

package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"

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
		return errAlreadyExists(to)
	}
	if isDir(&st) && isInside(to, from) {
		return errMoveIntoItself()
	}
	src := entryRef{dfd(sd), sname, from}
	dst := entryRef{dfd(dd), dname, to}
	err = renameNoReplace(src.dirfd, src.name, dst.dirfd, dst.name, to)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return err
	}
	return moveAcrossDevices(src, dst)
}

// moveAcrossDevices copies src to dst and then removes src, undoing the copy
// when it fails.
func moveAcrossDevices(src, dst entryRef) error {
	c := newCopier(context.Background(), nil)
	if err := c.copyAt(src, dst); err != nil {
		if c.madeTop {
			_ = removeAllAt(dst.dirfd, dst.name, dst.full)
		}
		return err
	}
	return removeAllAt(src.dirfd, src.name, src.full)
}

func doCopy(ctx context.Context, p copyParams, s rpc.Stream) error {
	from, to, err := copyTargets(p)
	if err != nil {
		return err
	}
	toDir, err := openDir(to)
	if err != nil {
		return err
	}
	defer toDir.Close()
	jobs, total, err := planCopy(ctx, p, from, to)
	if err != nil {
		return err
	}
	r := &copyReporter{s: s, total: total}
	if err := r.start(len(jobs)); err != nil {
		return err
	}
	results, err := r.run(jobs, func(j copyJob) error {
		return copyJobTo(ctx, p.Move, dfd(toDir), j, r)
	})
	if err != nil {
		return err
	}
	return r.finish(map[string]any{"done": true, "paths": results})
}

// planCopy checks every source and picks its free destination name inside to.
func planCopy(ctx context.Context, p copyParams, from []string, to string) ([]copyJob, int64, error) {
	var jobs []copyJob
	var total int64
	for _, src := range from {
		st, err := lstatPath(src)
		if err != nil {
			return nil, 0, err
		}
		if p.Move {
			if err := checkNotProtected(src); err != nil {
				return nil, 0, err
			}
		}
		if isDir(&st) && isInside(to, src) {
			return nil, 0, errCopyIntoItself()
		}
		if p.Move && filepath.Dir(src) == to {
			continue // already there
		}
		jobs = append(jobs, copyJob{src, uniqueName(filepath.Join(to, filepath.Base(src)))})
		sz, _ := treeSize(ctx, src)
		total += sz
	}
	return jobs, total, nil
}

// copyJobTo copies (or moves) one job into the folder open on toFd.
func copyJobTo(ctx context.Context, move bool, toFd int, j copyJob, r *copyReporter) error {
	sd, sname, err := openParent(j.src)
	if err != nil {
		return err
	}
	defer sd.Close()
	dname := filepath.Base(j.dst)
	if move {
		err := renameNoReplace(dfd(sd), sname, toFd, dname, j.dst)
		if err == nil {
			sz, _ := treeSize(ctx, j.dst)
			r.done += sz
			return nil
		}
		if !errors.Is(err, syscall.EXDEV) {
			return err
		}
	}
	c := newCopier(ctx, r.onBytes)
	if err := c.copyAt(entryRef{dfd(sd), sname, j.src}, entryRef{toFd, dname, j.dst}); err != nil {
		if c.madeTop {
			_ = removeAllAt(toFd, dname, j.dst)
		}
		return err
	}
	if r.err != nil {
		return r.err
	}
	if move {
		return removeAllAt(dfd(sd), sname, j.src)
	}
	return nil
}

// openParentStat opens the directory holding path and lstats the entry there.
// The caller closes the returned directory.
func openParentStat(path string) (*os.File, string, unix.Stat_t, error) {
	d, name, err := openParent(path)
	if err != nil {
		return nil, "", unix.Stat_t{}, err
	}
	st, err := lstatAt(dfd(d), name)
	if err != nil {
		d.Close()
		return nil, "", unix.Stat_t{}, pathErr("lstat", path, err)
	}
	return d, name, st, nil
}

func hChmod(ctx context.Context, c *rpc.Call) (any, error) {
	path, mode, recursive, err := bindChmod(c)
	if err != nil {
		return nil, err
	}
	d, name, st, err := openParentStat(path)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	if isLink(&st) {
		return nil, errChmodOnLink()
	}
	m := unixMode(mode)
	if !recursive || !isDir(&st) {
		if err := chmodNoFollow(dfd(d), name, m); err != nil {
			if errors.Is(err, errIsLink) {
				return nil, errChmodOnLink()
			}
			return nil, pathErr("chmod", path, err)
		}
		return map[string]any{"changed": 1}, nil
	}
	changed := 0
	var first firstErr
	err = walkAt(ctx, entryRef{dfd(d), name, path}, chmodVisitor(m, &changed), first.note)
	if err != nil {
		return nil, err
	}
	return changeResult(changed, first.err)
}

// chmodVisitor applies mode m to each visited entry and counts the changes.
func chmodVisitor(m uint32, changed *int) visitFunc {
	return func(dirfd int, name string, st *unix.Stat_t, self int) error {
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
			*changed++
		}
		return err
	}
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
	d, name, st, err := openParentStat(path)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	if !p.Recursive || !isDir(&st) {
		if err := unix.Fchownat(dfd(d), name, uid, gid, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return nil, pathErr("chown", path, err)
		}
		return map[string]any{"changed": 1}, nil
	}
	changed := 0
	var first firstErr
	err = walkAt(ctx, entryRef{dfd(d), name, path}, chownVisitor(uid, gid, &changed), first.note)
	if err != nil {
		return nil, err
	}
	if first.err != nil && changed == 0 {
		return nil, first.err
	}
	return map[string]any{"changed": changed}, nil
}

// chownVisitor gives each visited entry the owner uid and group gid (-1 keeps
// the current one) and counts the changes.
func chownVisitor(uid, gid int, changed *int) visitFunc {
	return func(dirfd int, name string, _ *unix.Stat_t, self int) error {
		var err error
		if self >= 0 {
			err = unix.Fchown(self, uid, gid)
		} else {
			err = unix.Fchownat(dirfd, name, uid, gid, unix.AT_SYMLINK_NOFOLLOW)
		}
		if err == nil {
			*changed++
		}
		return err
	}
}
