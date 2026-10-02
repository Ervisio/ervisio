package plugins

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// Limits of the plugin file methods.
const (
	MaxPluginFile    = 4 << 20 // bytes read or written by one call
	maxPluginListing = 2000    // entries returned by plugins.listDir
)

// FileParams are the params of plugins.readFile, plugins.writeFile and plugins.listDir.
type FileParams struct {
	Plugin string `json:"plugin"`
	Path   string `json:"path"`
	// Data (writeFile) is UTF-8 text, or base64 when B64 is true.
	Data string `json:"data,omitempty"`
	B64  bool   `json:"b64,omitempty"`
}

// FileResult is the result of plugins.readFile.
type FileResult struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	// Data is the content: UTF-8 text, or base64 when B64 is true (binary
	// content, or b64 asked for in the params).
	Data string `json:"data"`
	B64  bool   `json:"b64,omitempty"`
}

// DirEntry is one entry of plugins.listDir.
type DirEntry struct {
	Name  string `json:"name"`
	Type  string `json:"type"` // file, dir, link, other
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"` // unix ms
}

// capRoot is a declared folder (capabilities.files) a request falls in.
type capRoot struct {
	dir    string // absolute, ~ expanded, cleaned
	rel    string // path below dir, "." for the folder itself
	folder Folder // the manifest entry
}

// matchCap finds the longest declared folder that contains path and returns
// it with the relative rest. The relative part is opened through os.Root,
// so symlinks (or "..") inside it cannot leave the declared folder. On a
// tie the entry without admin wins, so the less privileged reading holds.
func matchCap(declared []Folder, path string) (capRoot, bool) {
	var best capRoot
	found := false
	for _, d := range declared {
		dir := filepath.Clean(expandHome(d.Path))
		if !filepath.IsAbs(dir) || strings.HasPrefix(dir, "~") {
			continue // ~ without a home directory: never matches
		}
		var rel string
		switch {
		case path == dir:
			rel = "."
		case dir == "/":
			rel = strings.TrimPrefix(path, "/")
		case strings.HasPrefix(path, dir+"/"):
			rel = path[len(dir)+1:]
		default:
			continue
		}
		if !found || len(dir) > len(best.dir) || (len(dir) == len(best.dir) && best.folder.Admin && !d.Admin) {
			best, found = capRoot{dir: dir, rel: rel, folder: d}, true
		}
	}
	return best, found
}

// pluginPath checks a path from a plugin and resolves it against the
// declared folders for the operation (read: files.read and files.write,
// write: files.write only). Plain folders are used with the user's own
// rights, never on the root bridge. Admin folders run on the root bridge,
// or as the user when the user is root or in adminUnlessGroup.
func pluginPath(c *rpc.Call, p FileParams, write bool) (*Found, capRoot, error) {
	f, who, err := authorize(c, p.Plugin)
	if err != nil {
		return nil, capRoot{}, err
	}
	raw := p.Path
	if raw == "" || len(raw) > 4096 || strings.ContainsRune(raw, 0) {
		return nil, capRoot{}, rpc.Errorf(rpc.Invalid, "Give an absolute path.")
	}
	path := filepath.Clean(expandHome(raw))
	if !filepath.IsAbs(path) {
		return nil, capRoot{}, rpc.Errorf(rpc.Invalid, "Give an absolute path (or one starting with ~/).")
	}
	caps := f.M.Capabilities.Files
	declared := caps.Write
	if !write {
		declared = append(append([]Folder{}, caps.Read...), caps.Write...)
	}
	r, ok := matchCap(declared, path)
	if !ok {
		verb := "read"
		if write {
			verb = "write"
		}
		return nil, capRoot{}, rpc.Errorf(rpc.Forbidden, "%s did not declare that it may %s %s.", f.M.Name, verb, path)
	}
	if c.Admin && !r.folder.Admin {
		// File capabilities use the signed-in user's own rights unless the
		// folder is declared admin.
		return nil, capRoot{}, rpc.Errorf(rpc.Forbidden, "Plugin file access runs with your own rights, not as administrator.")
	}
	root := c.Admin || os.Geteuid() == 0
	if r.folder.Admin && !root && !(r.folder.AdminUnlessGroup != "" && who.Groups[r.folder.AdminUnlessGroup]) {
		return nil, capRoot{}, rpc.Errorf(rpc.NeedsAdmin, "%s needs administrator rights to use %s.", f.M.Name, r.dir)
	}
	return f, r, nil
}

// ensureFolder creates a declared folder marked create:true when it is
// missing (0700 under ~, else 0755), before a write.
func ensureFolder(r capRoot) error {
	if !r.folder.Create {
		return nil
	}
	if _, err := os.Lstat(r.dir); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	mode := fs.FileMode(0o755)
	if strings.HasPrefix(r.folder.Path, "~") {
		mode = 0o700
	}
	if err := os.MkdirAll(r.dir, mode); err != nil {
		return fileErr(err, r.dir)
	}
	return nil
}

func fileErr(err error, path string) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return rpc.Errorf(rpc.NotFound, "%s does not exist.", path)
	case errors.Is(err, fs.ErrPermission):
		return rpc.Errorf(rpc.Forbidden, "You may not access %s.", path)
	}
	// os.Root refuses paths that escape the folder ("path escapes from parent").
	return rpc.Errorf(rpc.Forbidden, "%s is outside the folders the plugin declared.", path)
}

func readPluginFile(_ context.Context, c *rpc.Call) (any, error) {
	var p FileParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	_, r, err := pluginPath(c, p, false)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(r.dir)
	if err != nil {
		return nil, fileErr(err, p.Path)
	}
	defer root.Close()
	fh, err := root.Open(r.rel)
	if err != nil {
		return nil, fileErr(err, p.Path)
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		return nil, fileErr(err, p.Path)
	}
	if !fi.Mode().IsRegular() {
		return nil, rpc.Errorf(rpc.Invalid, "%s is not a regular file.", p.Path)
	}
	if fi.Size() > MaxPluginFile {
		return nil, rpc.Errorf(rpc.Invalid, "%s is larger than %d MiB.", p.Path, MaxPluginFile>>20)
	}
	b, err := io.ReadAll(io.LimitReader(fh, MaxPluginFile+1))
	if err != nil {
		return nil, fileErr(err, p.Path)
	}
	if len(b) > MaxPluginFile {
		return nil, rpc.Errorf(rpc.Invalid, "%s is larger than %d MiB.", p.Path, MaxPluginFile>>20)
	}
	res := &FileResult{Path: filepath.Join(r.dir, r.rel), Size: int64(len(b))}
	if p.B64 || !utf8.Valid(b) {
		res.Data, res.B64 = base64.StdEncoding.EncodeToString(b), true
	} else {
		res.Data = string(b)
	}
	return res, nil
}

func writePluginFile(_ context.Context, c *rpc.Call) (any, error) {
	var p FileParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	_, r, err := pluginPath(c, p, true)
	if err != nil {
		return nil, err
	}
	if r.rel == "." {
		return nil, rpc.Errorf(rpc.Invalid, "%s is a declared folder, not a file.", p.Path)
	}
	if err := ensureFolder(r); err != nil {
		return nil, err
	}
	data := []byte(p.Data)
	if p.B64 {
		if data, err = base64.StdEncoding.DecodeString(p.Data); err != nil {
			return nil, rpc.Errorf(rpc.Invalid, "data is not valid base64.")
		}
	}
	if len(data) > MaxPluginFile {
		return nil, rpc.Errorf(rpc.Invalid, "At most %d MiB can be written at once.", MaxPluginFile>>20)
	}
	root, err := os.OpenRoot(r.dir)
	if err != nil {
		return nil, fileErr(err, p.Path)
	}
	defer root.Close()
	mode := fs.FileMode(0o644)
	if fi, err := root.Lstat(r.rel); err == nil {
		if !fi.Mode().IsRegular() {
			return nil, rpc.Errorf(rpc.Invalid, "%s is not a regular file.", p.Path)
		}
		mode = fi.Mode().Perm()
	}
	// Write a temporary file next to the target and rename it over the
	// target. The folder is opened through os.Root and both names are
	// single components relative to its descriptor, so neither the write
	// nor the rename can leave the declared folder (a symlink at the target
	// is replaced, never followed).
	dirRel, base := filepath.Dir(r.rel), filepath.Base(r.rel)
	dh, err := root.Open(dirRel)
	if err != nil {
		return nil, fileErr(err, p.Path)
	}
	defer dh.Close()
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	tmpBase := ".la-plugin-" + hex.EncodeToString(rnd[:]) + ".tmp"
	tmp := filepath.Join(dirRel, tmpBase)
	fh, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return nil, fileErr(err, p.Path)
	}
	_, werr := fh.Write(data)
	cerr := fh.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		fd := int(dh.Fd())
		werr = syscall.Renameat(fd, tmpBase, fd, base)
	}
	if werr != nil {
		_ = root.Remove(tmp)
		return nil, fileErr(werr, p.Path)
	}
	return map[string]any{"path": filepath.Join(r.dir, r.rel), "size": len(data)}, nil
}

func listPluginDir(_ context.Context, c *rpc.Call) (any, error) {
	var p FileParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	_, r, err := pluginPath(c, p, false)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(r.dir)
	if err != nil {
		return nil, fileErr(err, p.Path)
	}
	defer root.Close()
	dh, err := root.Open(r.rel)
	if err != nil {
		return nil, fileErr(err, p.Path)
	}
	defer dh.Close()
	ents, err := dh.ReadDir(maxPluginListing)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, rpc.Errorf(rpc.Invalid, "%s is not a folder.", p.Path)
	}
	out := make([]DirEntry, 0, len(ents))
	for _, e := range ents {
		d := DirEntry{Name: e.Name(), Type: "other"}
		switch t := e.Type(); {
		case t.IsDir():
			d.Type = "dir"
		case t.IsRegular():
			d.Type = "file"
		case t&fs.ModeSymlink != 0:
			d.Type = "link"
		}
		if fi, err := e.Info(); err == nil {
			d.Size, d.MTime = fi.Size(), fi.ModTime().UnixMilli()
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return map[string]any{"path": filepath.Join(r.dir, r.rel), "entries": out}, nil
}

// mkdirPlugin implements plugins.mkdir: creates a folder (and missing
// parents) inside a files.write folder.
func mkdirPlugin(_ context.Context, c *rpc.Call) (any, error) {
	var p FileParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	_, r, err := pluginPath(c, p, true)
	if err != nil {
		return nil, err
	}
	if err := ensureFolder(r); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(r.dir)
	if err != nil {
		return nil, fileErr(err, p.Path)
	}
	defer root.Close()
	// One component at a time through os.Root (no MkdirAll before Go 1.25),
	// so a symlink on the way cannot lead out of the folder.
	cur := ""
	for _, part := range strings.Split(r.rel, "/") {
		if part == "." || part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		err := root.Mkdir(cur, 0o755)
		if err == nil {
			continue
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fileErr(err, p.Path)
		}
		fi, serr := root.Stat(cur)
		if serr != nil {
			return nil, fileErr(serr, p.Path)
		}
		if !fi.IsDir() {
			return nil, rpc.Errorf(rpc.Invalid, "%s exists and is not a folder.", filepath.Join(r.dir, cur))
		}
	}
	return map[string]any{"path": filepath.Join(r.dir, r.rel)}, nil
}

// removePlugin implements plugins.remove: deletes a file, a symlink or an
// empty folder inside a files.write folder (never the declared folder).
func removePlugin(_ context.Context, c *rpc.Call) (any, error) {
	var p FileParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	_, r, err := pluginPath(c, p, true)
	if err != nil {
		return nil, err
	}
	if r.rel == "." {
		return nil, rpc.Errorf(rpc.Invalid, "%s is a declared folder; it cannot be removed.", p.Path)
	}
	root, err := os.OpenRoot(r.dir)
	if err != nil {
		return nil, fileErr(err, p.Path)
	}
	defer root.Close()
	if err := root.Remove(r.rel); err != nil {
		if errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST) {
			return nil, rpc.Errorf(rpc.Invalid, "%s is a folder that is not empty.", p.Path)
		}
		return nil, fileErr(err, p.Path)
	}
	return map[string]any{"path": filepath.Join(r.dir, r.rel)}, nil
}
