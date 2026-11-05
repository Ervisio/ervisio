package files

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// isInside reports whether child is parent or lies below it.
func isInside(child, parent string) bool {
	if runtime.GOOS == "windows" {
		return isInsideWin(child, parent)
	}
	if parent == "/" {
		return true
	}
	return child == parent || strings.HasPrefix(child, parent+"/")
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

// streamHandler binds the call's params to P and runs do on them.
func streamHandler[P any](do func(context.Context, P, rpc.Stream) error) func(context.Context, *rpc.Call, rpc.Stream) error {
	return func(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
		var p P
		if err := c.Bind(&p); err != nil {
			return err
		}
		return do(ctx, p, s)
	}
}

var hCopy = streamHandler(doCopy)

// copyJob is one planned item of a copy or move.
type copyJob struct{ src, dst string }

const copyProgressInterval = 100 * time.Millisecond

func errCopyCancelled() error { return rpc.Errorf(rpc.Unavailable, "The copy was cancelled.") }

func errCopyIntoItself() error {
	return rpc.Errorf(rpc.Invalid, "A folder cannot be copied or moved into itself.")
}

func errMoveIntoItself() error {
	return rpc.Errorf(rpc.Invalid, "A folder cannot be moved into itself.")
}

func errAlreadyExists(path string) error {
	return rpc.Errorf(rpc.Conflict, "%s already exists. Choose another name.", path)
}

// copyBuffer lazily allocates the transfer buffer a copier reuses for its files.
type copyBuffer struct{ buf []byte }

func (b *copyBuffer) buffer() []byte {
	if b.buf == nil {
		b.buf = make([]byte, 256<<10)
	}
	return b.buf
}

// copyTargets cleans the sources and the destination folder of a copy.
func copyTargets(p copyParams) (from []string, to string, err error) {
	if from, err = cleanPaths(p.From); err != nil {
		return nil, "", err
	}
	if to, err = cleanPath(p.To); err != nil {
		return nil, "", err
	}
	ti, err := os.Stat(to)
	if err != nil {
		return nil, "", err
	}
	if !ti.IsDir() {
		return nil, "", rpc.Errorf(rpc.Invalid, "%s is not a folder.", to)
	}
	return from, to, nil
}

// copyReporter streams the start, progress and final messages of a copy.
type copyReporter struct {
	s     rpc.Stream
	total int64
	done  int64
	cur   string
	last  time.Time
	err   error // first failed progress send while bytes were being copied
}

func (r *copyReporter) start(items int) error {
	return r.s.Send(map[string]any{"type": "start", "total": r.total, "items": items})
}

func (r *copyReporter) emit(force bool) error {
	if force || time.Since(r.last) > copyProgressInterval {
		r.last = time.Now()
		return r.s.Send(map[string]any{"type": "progress", "done": r.done, "total": r.total, "current": r.cur})
	}
	return nil
}

// onBytes counts copied bytes; a failed send is kept in err for the caller.
func (r *copyReporter) onBytes(n int64) {
	r.done += n
	if r.err == nil {
		r.err = r.emit(false)
	}
}

// run reports and executes each job, returning the destinations in order.
func (r *copyReporter) run(jobs []copyJob, each func(copyJob) error) ([]string, error) {
	var results []string
	for _, j := range jobs {
		r.cur = filepath.Base(j.src)
		if err := r.emit(true); err != nil {
			return nil, err
		}
		if err := each(j); err != nil {
			return nil, err
		}
		results = append(results, j.dst)
	}
	return results, nil
}

func (r *copyReporter) finish(msg map[string]any) error {
	_ = r.emit(true)
	return r.s.Send(msg)
}

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
	trashed := []map[string]string{}
	failed := []failure{}
	var firstErr error
	for _, p := range paths {
		id, err := deleteOne(p, home, trash)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			re := rpc.ToError(err, false)
			failed = append(failed, failure{Path: p, Message: re.Message, Code: string(re.Code)})
			continue
		}
		deleted = append(deleted, p)
		if id != "" {
			trashed = append(trashed, map[string]string{"path": p, "id": id})
		}
	}
	if len(deleted) == 0 && firstErr != nil {
		return nil, firstErr // lets needs_admin reach the client
	}
	return map[string]any{"deleted": deleted, "failed": failed, "trashed": trashed}, nil
}

// deleteOne trashes or removes p after the safety checks.
func deleteOne(p, home string, trash bool) (string, error) {
	if err := checkNotProtected(p); err != nil {
		return "", err
	}
	if home != "" && p == home {
		return "", rpc.Errorf(rpc.Forbidden, "Your home folder cannot be deleted from here.")
	}
	if err := checkExists(p); err != nil {
		return "", err
	}
	if trash {
		return moveToTrash(p)
	}
	return "", removePath(p) // never follows symlinks, not even in parent folders as root
}

// homeOverride lets tests pick the home directory.
var homeOverride string

func homeDir() (string, error) {
	if homeOverride != "" {
		return homeOverride, nil
	}
	if runtime.GOOS == "windows" {
		if h := os.Getenv("USERPROFILE"); h != "" {
			return filepath.Clean(h), nil
		}
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

func errChmodOnLink() error {
	return rpc.Errorf(rpc.Invalid, "Permissions cannot be changed on a link. Change the item it points to instead.")
}

// bindChmod reads and validates the chmod request.
func bindChmod(c *rpc.Call) (path string, mode os.FileMode, recursive bool, err error) {
	var p struct {
		Path      string `json:"path"`
		Mode      string `json:"mode"`
		Recursive bool   `json:"recursive"`
	}
	if err = c.Bind(&p); err != nil {
		return "", 0, false, err
	}
	if path, err = cleanPath(p.Path); err != nil {
		return "", 0, false, err
	}
	if mode, err = parseMode(p.Mode); err != nil {
		return "", 0, false, err
	}
	return path, mode, p.Recursive, nil
}

// firstErr keeps the first error met while a recursive change goes on.
type firstErr struct{ err error }

func (f *firstErr) note(err error) {
	if f.err == nil {
		f.err = err
	}
}

// changeResult is the chmod answer: a warning when some entries failed, an
// error when none succeeded.
func changeResult(changed int, first error) (any, error) {
	if first != nil && changed == 0 {
		return nil, first
	}
	res := map[string]any{"changed": changed}
	if first != nil {
		res["warning"] = first.Error()
	}
	return res, nil
}
