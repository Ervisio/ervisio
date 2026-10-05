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

func hCopy(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p copyParams
	if err := c.Bind(&p); err != nil {
		return err
	}
	return doCopy(ctx, p, s)
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
			if e := checkExists(p); e != nil {
				err = e
			}
		}
		if err == nil {
			if trash {
				err = moveToTrash(p)
			} else {
				err = removePath(p) // never follows symlinks, not even in parent folders as root
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
