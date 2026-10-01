package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// appCSP is the Content-Security-Policy of the built web app. Everything is
// same-origin: plugins are ES modules under /plugins/, fonts are bundled.
const appCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; " +
	"frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"

// webHandler serves the built app with SPA fallback, or proxies to Vite.
func (s *Server) webHandler() http.Handler {
	if s.vite != nil {
		return s.vite
	}
	dir := s.opts.WebDir
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeErrorStatus(w, http.StatusMethodNotAllowed, rpc.Errorf(rpc.Invalid, "method not allowed"))
			return
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			http.Error(w, "web app not installed", http.StatusServiceUnavailable)
			return
		}
		defer root.Close()
		w.Header().Set("Content-Security-Policy", appCSP)
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && serveFile(w, r, root, name, strings.HasPrefix(name, "assets/")) {
			return
		}
		// SPA fallback: unknown paths without an extension get index.html;
		// a missing asset is a real 404.
		if name != "" && path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		if !serveFile(w, r, root, "index.html", false) {
			http.Error(w, "web app not installed", http.StatusServiceUnavailable)
		}
	})
}

// serveFile serves name from root; it returns false if it is not a file.
func serveFile(w http.ResponseWriter, r *http.Request, root *os.Root, name string, immutable bool) bool {
	f, err := root.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	if immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
	return true
}

func newViteProxy(target *url.URL, lg *log.Logger) http.Handler {
	p := httputil.NewSingleHostReverseProxy(target)
	p.ErrorLog = lg
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "Vite dev server not reachable at "+target.String()+
			" — run `npm --prefix web run dev`, or start linuxadmind with --web web/dist", http.StatusBadGateway)
	}
	return p
}

var pluginIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// handlePlugin serves /plugins/<id>/<file> from the plugin directories,
// confined to the plugin's folder (symlinks cannot escape it).
func (s *Server) handlePlugin(w http.ResponseWriter, r *http.Request, sess *Session) {
	id := r.PathValue("id")
	file := r.PathValue("file")
	if !pluginIDRe.MatchString(id) || file == "" || !fs.ValidPath(file) || strings.Contains(file, "\\") {
		writeError(w, errNotFound)
		return
	}
	for _, dir := range s.opts.PluginDirs {
		root, err := os.OpenRoot(path.Join(dir, id))
		if err != nil {
			continue
		}
		f, err := root.Open(file)
		if err != nil {
			root.Close()
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			writeError(w, errNotFound)
			return
		}
		fi, err := f.Stat()
		if err != nil || !fi.Mode().IsRegular() {
			f.Close()
			root.Close()
			continue
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Security-Policy", appCSP)
		http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
		f.Close()
		root.Close()
		return
	}
	s.serveDevPlugin(w, r, sess, id, file)
}

// maxDevAsset matches plugins.MaxDevAsset (the bridge refuses bigger files).
const maxDevAsset = 16 << 20

// serveDevPlugin serves a file of a folder the user loaded with
// plugins.loadDev. The daemon keeps no list of those folders: it asks the
// session's own user bridge (plugins.devAsset), which reads the file with
// the user's rights and confines it to the registered folder. Without a
// matching folder (or with developer mode off) the answer is 404.
func (s *Server) serveDevPlugin(w http.ResponseWriter, r *http.Request, sess *Session, id, file string) {
	const method = "plugins.devAsset"
	b, e := s.userBridge(r.Context(), sess)
	if e != nil {
		writeError(w, e)
		return
	}
	if _, ok := b.Level(method); !ok {
		writeError(w, errNotFound)
		return
	}
	st, err := b.Stream(r.Context(), method, map[string]string{"id": id, "file": file})
	if err != nil {
		writeError(w, errNotFound)
		return
	}
	defer st.Close()
	var meta fileMeta
	var buf bytes.Buffer
	first := true
	for ev := range st.Events() {
		if first {
			first = false
			if ev.B64 || json.Unmarshal(ev.Data, &meta) != nil || meta.Size < 0 || meta.Size > maxDevAsset {
				writeError(w, errNotFound)
				return
			}
			buf.Grow(int(meta.Size))
			continue
		}
		var b64 string
		if !ev.B64 || json.Unmarshal(ev.Data, &b64) != nil {
			writeError(w, errNotFound)
			return
		}
		chunk, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || int64(buf.Len()+len(chunk)) > meta.Size {
			writeError(w, errNotFound)
			return
		}
		buf.Write(chunk)
	}
	if first || st.Err() != nil || int64(buf.Len()) != meta.Size {
		writeError(w, errNotFound)
		return
	}
	if meta.Mime != "" {
		w.Header().Set("Content-Type", meta.Mime)
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", appCSP)
	http.ServeContent(w, r, path.Base(file), time.Time{}, bytes.NewReader(buf.Bytes()))
}

// redirectHandler answers plain-HTTP requests on the HTTPS port.
func redirectHandler(redirect bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !redirect {
			http.Error(w, "This server only speaks HTTPS.", http.StatusBadRequest)
			return
		}
		target := "https://" + r.Host + r.URL.RequestURI()
		w.Header().Set("Connection", "close")
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}
