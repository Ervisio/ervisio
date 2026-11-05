package remote

import (
	"context"
	"time"

	"github.com/ervisio/ervisio/server/internal/modules/files"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// side is one end of remote.copy: conn "" is this computer.
type side struct {
	Conn  string   `json:"conn"`
	Paths []string `json:"paths,omitempty"`
	Path  string   `json:"path,omitempty"`
}

type endpoint struct {
	fs    FS
	ops   pathOps
	local bool
	h     *handle
}

func (e *endpoint) release(err error) {
	if e.h != nil {
		e.h.release(err)
	}
}

func (m *module) endpoint(ctx context.Context, conn string) (*endpoint, error) {
	if conn == "" {
		return &endpoint{fs: localFS{}, ops: nativeOps, local: true}, nil
	}
	h, err := m.pool.get(ctx, conn)
	if err != nil {
		return nil, err
	}
	return &endpoint{fs: h, ops: posixOps, h: h}, nil
}

func (e *endpoint) clean(p string) (string, error) {
	if e.local {
		return cleanLocal(p)
	}
	return cleanRemote(p)
}

// maxWalk bounds the items counted before a copy starts.
const maxWalk = 200000

// hCopy copies or moves items between this computer and a connection, or
// between two connections (or within one). Events are those of files.copy:
// {"type":"start"}, {"type":"progress"}, then {"done":true,"paths":[…]}.
func (m *module) hCopy(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p struct {
		From side `json:"from"`
		To   side `json:"to"`
		Move bool `json:"move"`
	}
	if err := c.Bind(&p); err != nil {
		return err
	}
	if p.From.Conn == "" && p.To.Conn == "" {
		return rpc.Errorf(rpc.Invalid, "Use files.copy for copies on this computer.")
	}
	if len(p.From.Paths) == 0 {
		return rpc.Errorf(rpc.Invalid, "Nothing was selected.")
	}
	if len(p.From.Paths) > 10000 {
		return rpc.Errorf(rpc.Invalid, "Too many items at once (limit 10000).")
	}
	src, err := m.endpoint(ctx, p.From.Conn)
	if err != nil {
		return remoteErr(err)
	}
	var runErr error
	defer func() { src.release(runErr) }()
	dst, err := m.endpoint(ctx, p.To.Conn)
	if err != nil {
		return remoteErr(err)
	}
	defer func() { dst.release(runErr) }()
	runErr = m.copyRun(ctx, s, src, dst, p.From.Conn == p.To.Conn, p.From.Paths, p.To.Path, p.Move)
	return remoteErr(runErr)
}

func (m *module) copyRun(ctx context.Context, s rpc.Stream, src, dst *endpoint, same bool, from []string, toRaw string, move bool) error {
	to, err := dst.clean(toRaw)
	if err != nil {
		return err
	}
	te, err := dst.fs.Stat(ctx, to)
	if err != nil {
		return err
	}
	if !isDirLike(te) {
		return rpc.Errorf(rpc.Invalid, "%s is not a folder.", to)
	}
	jobs, total, err := planCopy(ctx, src, dst, same, from, to, move)
	if err != nil {
		return err
	}
	if err := s.Send(map[string]any{"type": "start", "total": total, "items": len(jobs)}); err != nil {
		return err
	}
	var done int64
	var last time.Time
	cur := ""
	var sendErr error
	emit := func(force bool) {
		if sendErr == nil && (force || time.Since(last) > 150*time.Millisecond) {
			last = time.Now()
			sendErr = s.Send(map[string]any{"type": "progress", "done": done, "total": total, "current": cur})
		}
	}
	cp := &copier{ctx: ctx, src: src, dst: dst, progress: func(n int64) { done += n; emit(false) }}
	results := []string{}
	for _, j := range jobs {
		cur = src.ops.base(j.src)
		emit(true)
		if sendErr != nil {
			return sendErr
		}
		if move && same {
			// One server: a rename, no data moves.
			err := dst.fs.Rename(ctx, j.src, j.dst)
			if err != nil {
				return err
			}
			sz, _, _ := treeSize(ctx, dst, j.dst, j.e, new(int))
			done += sz
			results = append(results, j.dst)
			continue
		}
		skipped, err := cp.copyTop(j)
		if err != nil {
			return err
		}
		if sendErr != nil {
			return sendErr
		}
		if move {
			if err := cp.removeMoved(j, skipped); err != nil {
				return err
			}
		}
		results = append(results, j.dst)
	}
	emit(true)
	out := map[string]any{"done": true, "paths": results}
	if len(cp.skipped) > 0 {
		out["skipped"] = cp.skipped
	}
	return s.Send(out)
}

type copyItem struct {
	src, dst string
	e        Entry
}

// planCopy validates every selected path and returns the items to copy with
// their unique destination names and the total size.
func planCopy(ctx context.Context, src, dst *endpoint, same bool, from []string, to string, move bool) ([]copyItem, int64, error) {
	var jobs []copyItem
	var total int64
	items := 0
	for _, raw := range from {
		sp, err := src.clean(raw)
		if err != nil {
			return nil, 0, err
		}
		if src.ops.dir(sp) == sp {
			return nil, 0, rpc.Errorf(rpc.Invalid, "The top folder cannot be copied or moved.")
		}
		if move && src.local {
			if err := files.CheckNotProtected(sp); err != nil {
				return nil, 0, err
			}
		}
		e, err := src.fs.Stat(ctx, sp)
		if err != nil {
			return nil, 0, err
		}
		if same && (to == sp || isInsidePath(to, sp, src.ops)) && isDirLike(e) {
			return nil, 0, rpc.Errorf(rpc.Invalid, "A folder cannot be copied or moved into itself.")
		}
		if move && same && src.ops.dir(sp) == to {
			continue // already there
		}
		dp, err := uniqueName(ctx, dst.fs, dst.ops, dst.ops.join(to, src.ops.base(sp)))
		if err != nil {
			return nil, 0, err
		}
		jobs = append(jobs, copyItem{sp, dp, e})
		sz, n, err := treeSize(ctx, src, sp, e, &items)
		if err != nil {
			return nil, 0, err
		}
		total += sz
		items += n
	}
	return jobs, total, nil
}

// copyTop copies one item and reports whether anything below it was skipped.
func (c *copier) copyTop(j copyItem) (bool, error) {
	c.madeTop = false
	before := len(c.skipped)
	if err := c.copy(j.src, j.e, j.dst, 0); err != nil {
		// Leave nothing half-copied at the top level.
		if c.madeTop {
			_ = removeAll(context.WithoutCancel(c.ctx), c.dst.fs, j.dst)
		}
		return false, err
	}
	return len(c.skipped) > before, nil
}

// removeMoved deletes the source of a finished move copy.
func (c *copier) removeMoved(j copyItem, skipped bool) error {
	if skipped {
		return rpc.Errorf(rpc.Invalid, "%s was copied but not removed: it holds links or special files that cannot be moved.", j.src)
	}
	return removeAll(c.ctx, c.src.fs, j.src)
}

func isInsidePath(child, parent string, ops pathOps) bool {
	for d := child; ; {
		nd := ops.dir(d)
		if nd == parent {
			return true
		}
		if nd == d {
			return false
		}
		d = nd
	}
}

// treeSize sums the files below p (links to folders are not followed).
func treeSize(ctx context.Context, ep *endpoint, p string, e Entry, seen *int) (int64, int, error) {
	if isFileLike(e) {
		return e.Size, 1, nil
	}
	if e.Type != "dir" {
		return 0, 1, nil
	}
	var size int64
	n := 1
	kids, err := ep.fs.List(ctx, p)
	if err != nil {
		return 0, 0, err
	}
	for _, k := range kids {
		if *seen+n > maxWalk {
			return size, n, nil
		}
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		sz, kn, err := treeSize(ctx, ep, ep.ops.join(p, k.Name), k, seen)
		if err != nil {
			return 0, 0, err
		}
		size += sz
		n += kn
	}
	return size, n, nil
}

type copier struct {
	ctx      context.Context
	src, dst *endpoint
	progress func(int64)
	madeTop  bool
	skipped  []string
}

func (c *copier) copy(sp string, e Entry, dp string, depth int) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	switch {
	case isFileLike(e):
		return c.copyFile(sp, e, dp, depth)
	case e.Type == "dir":
		return c.copyDir(sp, dp, depth)
	}
	c.skipped = append(c.skipped, sp)
	return nil
}

func (c *copier) copyFile(sp string, e Entry, dp string, depth int) error {
	if e.Type == "symlink" && c.src.local {
		// Like files.copy: links on this computer are not followed.
		c.skipped = append(c.skipped, sp)
		return nil
	}
	r, err := c.src.fs.Open(c.ctx, sp)
	if err != nil {
		return err
	}
	size := e.Size
	if e.Type == "symlink" {
		size = -1
	}
	_, err = writeFile(c.ctx, c.dst.fs, c.dst.ops, dp, r, size, false, c.progress)
	_ = r.Close()
	if err == nil && depth == 0 {
		c.madeTop = true
	}
	return err
}

func (c *copier) copyDir(sp, dp string, depth int) error {
	if depth > 256 {
		return rpc.Errorf(rpc.Invalid, "%s is nested too deeply.", sp)
	}
	if err := c.dst.fs.Mkdir(c.ctx, dp); err != nil {
		return err
	}
	if depth == 0 {
		c.madeTop = true
	}
	kids, err := c.src.fs.List(c.ctx, sp)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if err := c.copy(c.src.ops.join(sp, k.Name), k, c.dst.ops.join(dp, k.Name), depth+1); err != nil {
			return err
		}
	}
	return nil
}
