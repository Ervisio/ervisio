package envs

import (
	"context"
	"encoding/json"
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
// in a 0700 folder of that user, so nobody else can connect.
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
	if err := prepareTunnelDir(m.opts.TunnelDir, dir, uid, gid); err != nil {
		return "", errf("Could not prepare the tunnel folder: %v", err)
	}
	path := filepath.Join(dir, e.ID+".sock")
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return "", errf("Could not open the tunnel socket: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return "", err
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(path, uid, gid); err != nil {
			ln.Close()
			return "", err
		}
	}
	t := &tunnel{env: e.ID, path: path}
	t.ln = &countedListener{Listener: ln, t: t}
	t.touch()
	switch e.Kind {
	case KindPortainerAgent:
		h, err := m.agentProxy(e)
		if err != nil {
			ln.Close()
			return "", err
		}
		t.srv = &http.Server{Handler: h, ReadHeaderTimeout: 30 * time.Second, ErrorLog: m.opts.Log}
		go func() { _ = t.srv.Serve(t.ln) }()
	default:
		dial, err := m.dialFn(e)
		if err != nil {
			ln.Close()
			return "", err
		}
		go serveRaw(t.ln, dial, m.opts.Log)
	}
	m.tunnels[key] = t
	return path, nil
}

func prepareTunnelDir(base, dir string, uid, gid int) error {
	if err := os.MkdirAll(base, 0o711); err != nil {
		return err
	}
	_ = os.Chmod(base, 0o711)
	fi, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a folder", dir)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(dir, uid, gid); err != nil {
			return err
		}
	}
	return os.Chmod(dir, 0o700)
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
	rp.ErrorLog = m.opts.Log
	rp.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
	}
	return rp, nil
}
