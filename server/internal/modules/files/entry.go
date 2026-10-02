package files

import (
	"context"
	"mime"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Entry describes one file system object.
type Entry struct {
	Name       string `json:"name"`
	Path       string `json:"path,omitempty"`
	Type       string `json:"type"` // file | dir | symlink | other
	Size       int64  `json:"size"`
	Mode       string `json:"mode"` // octal, e.g. "0644" (with setuid/setgid/sticky digit)
	Perm       string `json:"perm"` // rwx string, e.g. "rw-r--r--"
	Owner      string `json:"owner"`
	Group      string `json:"group"`
	UID        int    `json:"uid"`
	GID        int    `json:"gid"`
	MTime      int64  `json:"mtime"` // unix milliseconds
	Target     string `json:"target,omitempty"`
	TargetType string `json:"targetType,omitempty"` // for symlinks: type of what it points to ("" when broken)
	Mime       string `json:"mime,omitempty"`
}

var (
	nameMu   sync.Mutex
	userName = map[int]string{}
	grpName  = map[int]string{}
)

func lookupUser(uid int) string {
	nameMu.Lock()
	defer nameMu.Unlock()
	if n, ok := userName[uid]; ok {
		return n
	}
	n := strconv.Itoa(uid)
	if u, err := user.LookupId(n); err == nil {
		n = u.Username
	}
	userName[uid] = n
	return n
}

func lookupGroup(gid int) string {
	nameMu.Lock()
	defer nameMu.Unlock()
	if n, ok := grpName[gid]; ok {
		return n
	}
	n := strconv.Itoa(gid)
	if g, err := user.LookupGroupId(n); err == nil {
		n = g.Name
	}
	grpName[gid] = n
	return n
}

func typeOf(m os.FileMode) string {
	switch {
	case m.IsRegular():
		return "file"
	case m.IsDir():
		return "dir"
	case m&os.ModeSymlink != 0:
		return "symlink"
	}
	return "other"
}

// modeString renders the permission bits like ls does, without the type letter.
func modeString(m os.FileMode) string {
	const rwx = "rwxrwxrwx"
	b := []byte("---------")
	for i := 0; i < 9; i++ {
		if m&(1<<uint(8-i)) != 0 {
			b[i] = rwx[i]
		}
	}
	if m&os.ModeSetuid != 0 {
		if b[2] == 'x' {
			b[2] = 's'
		} else {
			b[2] = 'S'
		}
	}
	if m&os.ModeSetgid != 0 {
		if b[5] == 'x' {
			b[5] = 's'
		} else {
			b[5] = 'S'
		}
	}
	if m&os.ModeSticky != 0 {
		if b[8] == 'x' {
			b[8] = 't'
		} else {
			b[8] = 'T'
		}
	}
	return string(b)
}

// modeOctal returns the four-digit octal permission string including special bits.
func modeOctal(m os.FileMode) string {
	v := uint32(m.Perm())
	if m&os.ModeSetuid != 0 {
		v |= 04000
	}
	if m&os.ModeSetgid != 0 {
		v |= 02000
	}
	if m&os.ModeSticky != 0 {
		v |= 01000
	}
	s := strconv.FormatUint(uint64(v), 8)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

func mimeOf(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return ""
	}
	if m, ok := extraMime[ext]; ok {
		return m
	}
	m := mime.TypeByExtension(ext)
	if i := strings.Index(m, ";"); i >= 0 {
		m = m[:i]
	}
	return m
}

var extraMime = map[string]string{
	".md": "text/markdown", ".go": "text/x-go", ".py": "text/x-python", ".rs": "text/x-rust",
	".sh": "text/x-shellscript", ".conf": "text/plain", ".log": "text/plain", ".toml": "text/x-toml",
	".yaml": "text/yaml", ".yml": "text/yaml", ".json": "application/json", ".ts": "text/x-typescript",
	".tsx": "text/x-typescript", ".jsx": "text/javascript", ".service": "text/plain", ".ini": "text/plain",
	".webp": "image/webp", ".svg": "image/svg+xml", ".avif": "image/avif", ".opus": "audio/ogg",
	".mkv": "video/x-matroska", ".7z": "application/x-7z-compressed", ".zst": "application/zstd",
	".xz": "application/x-xz", ".iso": "application/x-iso9660-image", ".deb": "application/vnd.debian.binary-package",
	".rpm": "application/x-rpm",
}

// makeEntry builds an Entry from a Lstat result. dir is the parent directory.
func makeEntry(dir string, fi os.FileInfo) Entry {
	e := Entry{
		Name:  fi.Name(),
		Type:  typeOf(fi.Mode()),
		Size:  fi.Size(),
		Mode:  modeOctal(fi.Mode()),
		Perm:  modeString(fi.Mode()),
		MTime: fi.ModTime().UnixMilli(),
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		e.UID, e.GID = int(st.Uid), int(st.Gid)
		e.Owner, e.Group = lookupUser(e.UID), lookupGroup(e.GID)
	}
	full := filepath.Join(dir, fi.Name())
	switch e.Type {
	case "symlink":
		if t, err := os.Readlink(full); err == nil {
			e.Target = t
		}
		if ti, err := os.Stat(full); err == nil {
			e.TargetType = typeOf(ti.Mode())
			e.Mime = mimeOf(fi.Name())
		}
	case "file":
		e.Mime = mimeOf(fi.Name())
	}
	return e
}

const maxListEntries = 50000

func hList(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Path       string `json:"path"`
		ShowHidden bool   `json:"showHidden"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	entries, truncated, err := listDir(path, p.ShowHidden)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	if path == "/" {
		parent = ""
	}
	return map[string]any{"path": path, "entries": entries, "parent": parent, "truncated": truncated}, nil
}

func listDir(path string, showHidden bool) ([]Entry, bool, error) {
	// A symlink to a directory is fine to open; a plain file gets a clear message.
	fi, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if !fi.IsDir() {
		return nil, false, rpc.Errorf(rpc.Invalid, "%s is a file, not a folder.", path)
	}
	d, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer d.Close()
	out := []Entry{}
	truncated := false
	for {
		infos, err := d.Readdir(512)
		for _, fi := range infos {
			if !showHidden && strings.HasPrefix(fi.Name(), ".") {
				continue
			}
			if len(out) >= maxListEntries {
				truncated = true
				break
			}
			out = append(out, makeEntry(path, fi))
		}
		if err != nil || truncated {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := isDirLike(out[i]), isDirLike(out[j])
		if di != dj {
			return di
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, truncated, nil
}

func isDirLike(e Entry) bool {
	return e.Type == "dir" || (e.Type == "symlink" && e.TargetType == "dir")
}

func statEntry(path string) (Entry, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return Entry{}, err
	}
	e := makeEntry(filepath.Dir(path), fi)
	if path == "/" {
		e.Name = "/"
	}
	e.Path = path
	return e, nil
}

func hStat(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	return statEntry(path)
}

func hMkdir(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Path    string `json:"path"`
		Parents bool   `json:"parents"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	if p.Parents {
		if err := mkdirAll(path, 0o755); err != nil {
			return nil, err
		}
	} else {
		d, name, err := openParent(path)
		if err != nil {
			return nil, err
		}
		err = unix.Mkdirat(dfd(d), name, 0o755)
		d.Close()
		if err != nil {
			return nil, pathErr("mkdir", path, err)
		}
	}
	return statEntry(path)
}

func hCreate(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	d, name, err := openParent(path)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat(dfd(d), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o644)
	d.Close()
	if err != nil {
		return nil, pathErr("create", path, err)
	}
	unix.Close(fd)
	return statEntry(path)
}

func hRename(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	from, err := cleanPath(p.From)
	if err != nil {
		return nil, err
	}
	to, err := cleanPath(p.To)
	if err != nil {
		return nil, err
	}
	if err := movePath(from, to); err != nil {
		return nil, err
	}
	return statEntry(to)
}
