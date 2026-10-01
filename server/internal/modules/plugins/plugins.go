// Package plugins implements the bridge methods of the Plugins section (plugins.*):
// listing installed plugins, enabling, installing and removing them, the
// catalog for Browse, and plugins.exec, which runs only the argv a plugin's
// manifest declares. See docs/api/plugins.md.
package plugins

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// Register adds the plugins.* methods to the registry.
func Register(r *rpc.Registry) {
	r.Handle("plugins.list", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return list(c.Admin), nil
	})

	r.Handle("plugins.setEnabled", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		f := find(readPolicy(), p.ID)
		if f == nil {
			return nil, rpc.Errorf(rpc.NotFound, "There is no plugin %q.", p.ID)
		}
		st := readState()
		st.Enabled[p.ID] = p.Enabled
		if err := writeState(st); err != nil {
			return nil, rpc.Errorf(rpc.Internal, "Could not save the plugin state: %v", cleanErr(err))
		}
		return map[string]any{"id": p.ID, "enabled": p.Enabled}, nil
	})

	r.Handle("plugins.uninstall", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		if err := uninstall(p.ID); err != nil {
			return nil, err
		}
		return map[string]any{"id": p.ID}, nil
	})

	r.Handle("plugins.install", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p installRequest
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return install(ctx, p)
	})

	r.Handle("plugins.catalog", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return catalogView(ctx)
	})

	r.Handle("plugins.exec", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p ExecParams
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return runExec(ctx, c, p)
	})

	r.Stream("plugins.execStream", rpc.User, func(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
		var p ExecParams
		if err := c.Bind(&p); err != nil {
			return err
		}
		return runStream(ctx, c, s, p)
	})

	r.Handle("plugins.loadDev", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			Path string `json:"path"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return loadDev(p.Path)
	})

	r.Handle("plugins.unloadDev", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			Path string `json:"path"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return unloadDev(p.Path)
	})
}

// DevInfo is the result of plugins.loadDev.
type DevInfo struct {
	Path   string `json:"path"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Linked bool   `json:"linked"`
	Note   string `json:"note,omitempty"`
}

func loadDev(raw string) (*DevInfo, error) {
	pol := readPolicy()
	if !devEnabled(pol) {
		return nil, rpc.Errorf(rpc.Forbidden, "Developer mode is off. Set plugins.dev = true in Settings > Server (or run the daemon with --dev) to load plugins from a folder.")
	}
	if raw == "" {
		return nil, rpc.Errorf(rpc.Invalid, "Give the path of the plugin folder.")
	}
	dir := filepath.Clean(expandHome(raw))
	if !filepath.IsAbs(dir) {
		return nil, rpc.Errorf(rpc.Invalid, "Use an absolute path (or one starting with ~/).")
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil, rpc.Errorf(rpc.NotFound, "%s is not a folder.", dir)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "%s is not a valid plugin folder: %v", dir, err)
	}
	for _, f := range scan(pol) {
		if f.M != nil && f.M.ID == m.ID && f.Location != LocDev {
			return nil, rpc.Errorf(rpc.Conflict, "A plugin with the id %q is already installed. Uninstall it, or change the id in your manifest.", m.ID)
		}
	}
	st := readDev()
	have := false
	for _, f := range st.Folders {
		if f == dir {
			have = true
		}
	}
	if !have {
		st.Folders = append(st.Folders, dir)
		if err := saveDev(st); err != nil {
			return nil, rpc.Errorf(rpc.Internal, "Could not remember the folder: %v", cleanErr(err))
		}
	}
	info := &DevInfo{Path: dir, ID: m.ID, Name: m.Name}
	// The daemon serves /plugins/<id>/ from fixed folders. In dev, link the folder into the first one.
	if dd := devDirs(pol); len(dd) > 0 {
		link := filepath.Join(dd[0], m.ID)
		if filepath.Clean(link) == dir {
			info.Linked = true
		} else if _, err := os.Lstat(link); err == nil {
			if t, err := os.Readlink(link); err == nil && t == dir {
				info.Linked = true
			} else {
				info.Note = link + " already exists, so the folder is not linked into the dev plugins folder."
			}
		} else if err := os.Symlink(dir, link); err == nil {
			info.Linked = true
		} else {
			info.Note = "Could not link the folder into " + dd[0] + ": " + cleanErr(err)
		}
	}
	return info, nil
}

func unloadDev(raw string) (any, error) {
	dir := filepath.Clean(expandHome(raw))
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	st := readDev()
	out := st.Folders[:0]
	for _, f := range st.Folders {
		if f != dir {
			out = append(out, f)
		}
	}
	st.Folders = out
	if err := saveDev(st); err != nil {
		return nil, rpc.Errorf(rpc.Internal, "Could not save the folder list: %v", cleanErr(err))
	}
	for _, d := range devDirs(readPolicy()) {
		if m, err := LoadManifest(dir); err == nil {
			l := filepath.Join(d, m.ID)
			if t, err := os.Readlink(l); err == nil && t == dir {
				_ = os.Remove(l)
			}
		}
	}
	return map[string]any{"path": dir}, nil
}
