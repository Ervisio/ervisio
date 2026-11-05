// Package plugins implements the bridge methods of the Plugins section (plugins.*):
// listing installed plugins, enabling, installing and removing them, the
// catalog for Browse, and plugins.exec, which runs only the argv a plugin's
// manifest declares. See docs/api/plugins.md.
package plugins

import (
	"context"
	"os"
	"path/filepath"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// bound wraps a handler that takes its params already bound.
func bound[P any](fn func(ctx context.Context, c *rpc.Call, p P) (any, error)) rpc.Handler {
	return func(ctx context.Context, c *rpc.Call) (any, error) {
		var p P
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return fn(ctx, c, p)
	}
}

// boundStream is bound for stream handlers.
func boundStream[P any](fn func(ctx context.Context, c *rpc.Call, s rpc.Stream, p P) error) rpc.StreamHandler {
	return func(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
		var p P
		if err := c.Bind(&p); err != nil {
			return err
		}
		return fn(ctx, c, s, p)
	}
}

// pathParams are the params of plugins.loadDev and plugins.unloadDev.
type pathParams struct {
	Path string `json:"path"`
}

// Register adds the plugins.* methods to the registry.
func Register(r *rpc.Registry) {
	r.Handle("plugins.list", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return list(c.Admin), nil
	})
	r.Handle("plugins.setEnabled", rpc.Admin, bound(setEnabled))
	r.Handle("plugins.uninstall", rpc.Admin, bound(func(_ context.Context, _ *rpc.Call, p struct {
		ID string `json:"id"`
	}) (any, error) {
		if err := uninstall(p.ID); err != nil {
			return nil, err
		}
		return map[string]any{"id": p.ID}, nil
	}))
	r.Handle("plugins.install", rpc.Admin, bound(func(ctx context.Context, _ *rpc.Call, p installRequest) (any, error) {
		return install(ctx, p)
	}))
	r.Handle("plugins.catalog", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return catalogView(ctx)
	})
	r.Handle("plugins.exec", rpc.User, bound(func(ctx context.Context, c *rpc.Call, p ExecParams) (any, error) {
		return runExec(ctx, c, p)
	}))
	r.Stream("plugins.execStream", rpc.User, boundStream(runStream))
	r.Stream("plugins.pty", rpc.User, boundStream(runPTY))

	// HTTP APIs on unix sockets, only for the declared rules (see http.go).
	r.Handle("plugins.http", rpc.User, bound(func(ctx context.Context, c *rpc.Call, p HTTPParams) (any, error) {
		return runHTTP(ctx, c, p)
	}))
	r.Stream("plugins.httpStream", rpc.User, boundStream(runHTTPStream))

	// Large transfers: streamed GET downloads (HTTP API or command output)
	// and uploads as a request body (see transfer.go).
	r.Stream("plugins.httpDownload", rpc.User, boundStream(runHTTPDownload))
	r.Stream("plugins.execDownload", rpc.User, boundStream(runExecDownload))
	r.Stream("plugins.httpUpload", rpc.User, boundStream(runHTTPUpload))

	// Plugin-scoped file access: only inside capabilities.files, with the
	// user's own rights, or on the root bridge for folders declared admin
	// (see files.go).
	r.Handle("plugins.readFile", rpc.User, readPluginFile)
	r.Handle("plugins.writeFile", rpc.User, writePluginFile)
	r.Handle("plugins.listDir", rpc.User, listPluginDir)
	r.Handle("plugins.mkdir", rpc.User, mkdirPlugin)
	r.Handle("plugins.remove", rpc.User, removePlugin)

	// Used by the daemon before serving plugin assets or the plugin frame.
	r.Handle("plugins.access", rpc.User, access)

	// Environments (envbridge.go) and user-approved network hosts (userhosts.go).
	registerEnvs(r)

	r.Handle("plugins.loadDev", rpc.User, bound(func(_ context.Context, _ *rpc.Call, p pathParams) (any, error) {
		return loadDev(p.Path)
	}))
	r.Stream("plugins.devAsset", rpc.User, devAsset)
	r.Handle("plugins.unloadDev", rpc.User, bound(func(_ context.Context, _ *rpc.Call, p pathParams) (any, error) {
		return unloadDev(p.Path)
	}))
}

// setEnabled implements plugins.setEnabled.
func setEnabled(_ context.Context, _ *rpc.Call, p struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}) (any, error) {
	f := find(readPolicy(), p.ID)
	if f == nil {
		return nil, rpc.Errorf(rpc.NotFound, "There is no plugin %q.", p.ID)
	}
	if msg := f.M.CoreProblem(); msg != "" && p.Enabled {
		return nil, rpc.Errorf(rpc.Conflict, "%s", msg)
	}
	st := readState()
	st.Enabled[p.ID] = p.Enabled
	if err := writeState(st); err != nil {
		return nil, rpc.Errorf(rpc.Internal, "Could not save the plugin state: %v", cleanErr(err))
	}
	return map[string]any{"id": p.ID, "enabled": p.Enabled}, nil
}

// DevInfo is the result of plugins.loadDev.
type DevInfo struct {
	Path string `json:"path"`
	ID   string `json:"id"`
	Name string `json:"name"`
	// Linked is always true: the daemon serves the folder's files at
	// /plugins/<id>/ (kept for older clients).
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
	dir, err := devDir(raw)
	if err != nil {
		return nil, err
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
	if err := rememberDev(dir); err != nil {
		return nil, err
	}
	// The daemon serves /plugins/<id>/… of loaded folders through
	// plugins.devAsset on the user's own bridge, so nothing is linked.
	info := &DevInfo{Path: dir, ID: m.ID, Name: m.Name, Linked: true}
	return info, nil
}

// devDir validates the folder given to plugins.loadDev and resolves symlinks.
func devDir(raw string) (string, error) {
	dir := filepath.Clean(expandHome(raw))
	if !filepath.IsAbs(dir) {
		return "", rpc.Errorf(rpc.Invalid, "Use an absolute path (or one starting with ~/).")
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return "", rpc.Errorf(rpc.NotFound, "%s is not a folder.", dir)
	}
	return dir, nil
}

// rememberDev adds dir to the saved developer folders when it is new.
func rememberDev(dir string) error {
	st := readDev()
	for _, f := range st.Folders {
		if f == dir {
			return nil
		}
	}
	st.Folders = append(st.Folders, dir)
	if err := saveDev(st); err != nil {
		return rpc.Errorf(rpc.Internal, "Could not remember the folder: %v", cleanErr(err))
	}
	return nil
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
	return map[string]any{"path": dir}, nil
}
