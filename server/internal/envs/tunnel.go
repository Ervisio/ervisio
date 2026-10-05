package envs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// tunnelIdle is how long a tunnel without connections stays before it is closed.
const tunnelIdle = 10 * time.Minute

// tunnel is a unix socket the daemon serves for one user and one
// environment. Whatever connects speaks the Docker HTTP API; the daemon
// carries it to the environment. The socket is 0600 and owned by the user,
// so nobody else can connect. It lives in <TunnelDir>/<uid>, a folder that
// root owns (0711: the user can reach a socket whose name it knows, but not
// list or change the folder), so the user can never put a symlink where the
// daemon (root) creates, chowns or removes the socket (security review H1).
type tunnel struct {
	env  string
	path string
	ln   net.Listener
	srv  *http.Server // portainer-agent only

	active  atomic.Int64
	lastUse atomic.Int64
	once    sync.Once
}

func (t *tunnel) touch() { t.lastUse.Store(time.Now().UnixNano()) }

func (t *tunnel) close() {
	t.once.Do(func() {
		if t.srv != nil {
			_ = t.srv.Close()
		}
		_ = t.ln.Close()
		_ = os.Remove(t.path)
	})
}

// countedListener counts live connections.
type countedListener struct {
	net.Listener
	t *tunnel
}

type countedConn struct {
	net.Conn
	t    *tunnel
	once sync.Once
}

func (l *countedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.t.active.Add(1)
	l.t.touch()
	return &countedConn{Conn: c, t: l.t}, nil
}

func (c *countedConn) Close() error {
	c.once.Do(func() { c.t.active.Add(-1); c.t.touch() })
	return c.Conn.Close()
}

// CloseWrite forwards a half-close.
func (c *countedConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// Tunnel returns the path of the unix socket through which uid can reach
// the environment (created on first use, kept while it is used).
func (m *Manager) Tunnel(e *Env, uid, gid int) (string, error) {
	if e.Kind == KindErvisio {
		return "", errf("An Ervisio environment has no tunnel; its calls run on the other server.")
	}
	key := e.ID + ":" + strconv.Itoa(uid)
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.tunnels[key]; t != nil {
		t.touch()
		return t.path, nil
	}
	dir := filepath.Join(m.opts.TunnelDir, strconv.Itoa(uid))
	if err := prepareTunnelDir(m.opts.TunnelDir, dir); err != nil {
		return "", errf("Could not prepare the tunnel folder: %v", err)
	}
	path := filepath.Join(dir, e.ID+".sock")
	if len(path) > 100 { // sun_path holds 107 bytes; the bridge checks the same
		return "", errf("The tunnel folder path is too long (%d characters, at most 100): %s", len(path), path)
	}
	ln, err := listenOwned(m.opts.TunnelDir, path, uid, gid)
	if err != nil {
		return "", errf("Could not open the tunnel socket: %v", err)
	}
	t := &tunnel{env: e.ID, path: path}
	t.ln = &countedListener{Listener: ln, t: t}
	t.touch()
	switch e.Kind {
	case KindPortainerAgent:
		h, err := m.agentProxy(e)
		if err != nil {
			t.close()
			return "", err
		}
		t.srv = &http.Server{Handler: h, ReadHeaderTimeout: 30 * time.Second, ErrorLog: m.opts.Log}
		go func() { _ = t.srv.Serve(t.ln) }()
	default:
		dial, err := m.dialFn(e)
		if err != nil {
			t.close()
			return "", err
		}
		go serveRaw(t.ln, dial, m.opts.Log)
	}
	m.tunnels[key] = t
	return path, nil
}

// tunnelDirMode is the mode of the tunnel folders: root's, traversable.
const tunnelDirMode = 0o711

// ownedByUs reports whether fi is owned by the daemon's user (root in
// production).
func ownedByUs(fi os.FileInfo) bool {
	uid, _, ok := fileOwner(fi)
	return ok && uid == os.Geteuid()
}

// prepareTunnelDir makes base and dir folders of the daemon's user with
// mode 0711, nothing else: not a symlink, not the user's. A folder left
// from an older version (the user's own, 0700) is removed with whatever
// the user put in it (os.RemoveAll does not follow symlinks) and made
// again. base is made by the daemon only, so nobody else can create or
// rename entries in it.
func prepareTunnelDir(base, dir string) error {
	if err := os.MkdirAll(base, tunnelDirMode); err != nil {
		return err
	}
	bi, err := os.Lstat(base)
	if err != nil {
		return err
	}
	if !bi.IsDir() || !ownedByUs(bi) {
		return fmt.Errorf("%s is not a folder of this service", base)
	}
	if err := os.Chmod(base, tunnelDirMode); err != nil {
		return err
	}
	fi, err := os.Lstat(dir)
	if err == nil && (!fi.IsDir() || !ownedByUs(fi) || fi.Mode().Perm() != tunnelDirMode) {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		fi, err = nil, os.ErrNotExist
	}
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(dir, tunnelDirMode); err != nil {
			return err
		}
		if err := os.Chmod(dir, tunnelDirMode); err != nil { // umask
			return err
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() || !ownedByUs(fi) || fi.Mode().Perm() != tunnelDirMode {
		return fmt.Errorf("%s is not a folder of this service", dir)
	}
	return nil
}

// listenOwned listens on a unix socket at path that belongs to uid:gid with
// mode 0600. The socket is bound in a new folder of base that only the
// daemon can enter, given its owner and mode there, and only then renamed to
// path, so nobody can swap it for a symlink while the daemon changes it.
// The result is checked again with Lstat.
func listenOwned(base, path string, uid, gid int) (net.Listener, error) {
	tmp, err := os.MkdirTemp(base, ".new-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o700); err != nil {
		return nil, err
	}
	staged := filepath.Join(tmp, "s")
	ln, err := net.Listen("unix", staged)
	if err != nil {
		return nil, err
	}
	// The socket file moves; tunnel.close removes it at its final path.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	fail := func(err error) (net.Listener, error) {
		ln.Close()
		return nil, err
	}
	if err := os.Chmod(staged, 0o600); err != nil {
		return fail(err)
	}
	if os.Geteuid() == 0 {
		if err := os.Lchown(staged, uid, gid); err != nil {
			return fail(err)
		}
	}
	// A socket left by an earlier run (the daemon's folder: no one else's).
	if fi, err := os.Lstat(path); err == nil && !fi.IsDir() {
		_ = os.Remove(path)
	}
	if err := os.Rename(staged, path); err != nil {
		return fail(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return fail(err)
	}
	fuid, fgid, ok := fileOwner(fi)
	if fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 || !ok ||
		(os.Geteuid() == 0 && (fuid != uid || fgid != gid)) {
		_ = os.Remove(path)
		return fail(fmt.Errorf("the socket %s did not get the expected owner and mode", path))
	}
	return ln, nil
}

// closeTunnels closes every tunnel of an environment (it changed or was removed).
func (m *Manager) closeTunnels(envID string) {
	m.mu.Lock()
	var ts []*tunnel
	for k, t := range m.tunnels {
		if t.env == envID {
			ts = append(ts, t)
			delete(m.tunnels, k)
		}
	}
	m.mu.Unlock()
	for _, t := range ts {
		t.close()
	}
}

func (m *Manager) gcTunnels() {
	m.mu.Lock()
	var ts []*tunnel
	for k, t := range m.tunnels {
		if t.active.Load() == 0 && time.Since(time.Unix(0, t.lastUse.Load())) > tunnelIdle {
			ts = append(ts, t)
			delete(m.tunnels, k)
		}
	}
	m.mu.Unlock()
	for _, t := range ts {
		t.close()
	}
}

// TunnelCount is the number of open tunnels (tests, diagnostics).
func (m *Manager) TunnelCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tunnels)
}

func serveRaw(ln net.Listener, dial func(context.Context) (net.Conn, error), lg *log.Logger) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*dialTimeout)
			up, err := dial(ctx)
			cancel()
			if err != nil {
				writeBadGateway(c, err)
				c.Close()
				return
			}
			pipe(c, up)
		}()
	}
}

// writeBadGateway answers an HTTP client whose connection could not be
// carried on, so the Docker CLI and the plugin see why, not just EOF.
func writeBadGateway(w io.Writer, err error) {
	body, _ := json.Marshal(map[string]string{"message": err.Error()})
	fmt.Fprintf(w, "HTTP/1.1 502 Bad Gateway\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
}

// pipe copies both ways until both sides are done, then closes both.
func pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			dst.Close()
		}
	}
	wg.Add(2)
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
	a.Close()
	b.Close()
}

// agentProxy is the handler of a portainer-agent tunnel: every request is
// signed and sent to the agent over pinned TLS; protocol upgrades (attach,
// exec) and streaming bodies pass through.
func (m *Manager) agentProxy(e *Env) (http.Handler, error) {
	rt, err := m.agentTransport(e)
	if err != nil {
		return nil, err
	}
	target := &url.URL{Scheme: "http", Host: "agent"}
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = rt
	rp.FlushInterval = -1
	// net/http's chunked reader keeps reading inside one HTTP chunk until the
	// caller's buffer is full. The agent proxies a Docker stream as one big
	// chunk, so a 32 KiB copy buffer (the default) holds a log or stats
	// stream back until 32 KiB arrived, minutes for a quiet container. A
	// small buffer returns what the agent sent as it comes.
	rp.BufferPool = smallBuffers{}
	rp.ErrorLog = m.opts.Log
	rp.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
	}
	// The agent's proxy cannot carry a hijacked (upgraded) connection: it
	// answers 101 and then drops the stream, so attach, foreground run and
	// exec would hang or end empty. Refuse them with a message instead.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "A Portainer agent cannot carry attach, exec or interactive streams (it drops them). Use SSH or Docker over TLS for those, or run the container detached."})
			return
		}
		rp.ServeHTTP(w, r)
	}), nil
}

// smallBuffers hands httputil.ReverseProxy a 1 KiB copy buffer (see agentProxy).
type smallBuffers struct{}

func (smallBuffers) Get() []byte { return make([]byte, 1024) }
func (smallBuffers) Put([]byte)  {}
