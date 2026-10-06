package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const hangManifest = `{"id":"hang","name":"Hang","version":"1.0.0","entry":"index.js","platforms":["linux","windows"],
 "capabilities":{"http":[{"name":"svc","socket":"%SOCK%","admin":false,"headers":[],
    "rules":[{"methods":["PUT"],"path":"/archive"}],"maxUpload":200000000,"timeoutSec":600}],
  "files":{"read":[],"write":[]},"sockets":[],"network":[]},
 "contributes":{"pages":[{"id":"main","title":"Hang"}],"widgets":[],"snippets":[]},
 "visibleTo":{"groups":[]}}`

// hangService is a unix socket service that reads a request's body and never
// answers (Docker waiting for the rest of an archive): closed gets a value
// when the client closes the connection.
func hangService(t *testing.T) (sock string, closed chan struct{}, got chan int64) {
	dir, err := os.MkdirTemp("", "hang")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock = filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	closed, got = make(chan struct{}, 4), make(chan int64, 64)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 64<<10)
				var n int64
				for {
					k, err := c.Read(buf)
					n += int64(k)
					if k > 0 {
						select {
						case got <- n:
						default:
						}
					}
					if err != nil {
						closed <- struct{}{}
						return
					}
				}
			}()
		}
	}()
	return sock, closed, got
}

func newHangEnv(t *testing.T, sock string) (*Server, *httptest.Server, *http.Client) {
	plugins := t.TempDir()
	p := filepath.Join(plugins, "hang")
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "manifest.json"), []byte(strings.ReplaceAll(hangManifest, "%SOCK%", strings.ReplaceAll(sock, `\`, `\\`))), 0o644)
	os.WriteFile(filepath.Join(p, "index.js"), []byte("export default () => {}"), 0o644)
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html>app"), 0o644)
	srv, err := New(Options{ConfigPath: filepath.Join(t.TempDir(), "ervisio.conf"), Dev: true, NoAuth: true, WebDir: web,
		Bridge: buildBridge(t), PluginDirs: []string{plugins}, DevPluginsDir: plugins, StateDir: t.TempDir(), Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		for _, s := range srv.sessions.all() {
			srv.sessions.remove(s)
		}
	})
	return srv, ts, noAuthClient(t, srv, ts)
}

// startHangUpload asks for an upload link of size bytes to PUT /archive.
func startHangUpload(t *testing.T, ts *httptest.Server, cl *http.Client, size int, stream bool) string {
	b, _ := json.Marshal(map[string]any{"kind": "upload", "plugin": "hang", "name": "svc", "method": "PUT", "path": "/archive", "size": size, "stream": stream})
	req, _ := http.NewRequest("POST", ts.URL+"/api/plugins/transfer", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ervisio")
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Result struct {
			URL string `json:"url"`
		} `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if out.Result.URL == "" {
		t.Fatalf("start: %d", resp.StatusCode)
	}
	return ts.URL + out.Result.URL
}

// stallingBody sends n bytes, then blocks until the context ends.
type stallingBody struct {
	ctx  context.Context
	left int
}

func (s *stallingBody) Read(p []byte) (int, error) {
	if s.left == 0 {
		<-s.ctx.Done()
		return 0, s.ctx.Err()
	}
	k := min(len(p), s.left)
	for i := range p[:k] {
		p[i] = 'x'
	}
	s.left -= k
	return k, nil
}

// An upload the browser abandons halfway (the tab is closed, the frame
// aborts) must close the bridge's request to the service: Docker would
// otherwise wait for the rest of the body and keep the container locked.
func TestAbandonedUploadClosesTheServiceConnection(t *testing.T) {
	for _, c := range []struct{ stream, endSession bool }{{false, false}, {true, false}, {false, true}} {
		stream := c.stream
		sock, closed, got := hangService(t)
		srv, ts, cl := newHangEnv(t, sock)
		url := startHangUpload(t, ts, cl, 50<<20, stream)
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, "POST", url, &stallingBody{ctx: ctx, left: 1 << 20})
		req.ContentLength = 50 << 20
		req.Header.Set("X-Requested-With", "ervisio")
		done := make(chan struct{})
		go func() {
			if resp, err := cl.Do(req); err == nil {
				resp.Body.Close()
			}
			close(done)
		}()
		// Wait until the service has part of the body, then abandon.
		deadline := time.After(10 * time.Second)
	wait:
		for {
			select {
			case n := <-got:
				if n >= 512<<10 {
					break wait
				}
			case <-deadline:
				t.Fatalf("stream=%v: the service got nothing", stream)
			}
		}
		if c.endSession {
			// The session ends (sign-out elsewhere, expiry) while the
			// browser still holds the request open.
			srv.sessions.remove(srv.sessions.all()[0])
		} else {
			cancel()
			<-done
		}
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatalf("%+v: the connection to the service stayed open after the upload was abandoned", c)
		}
		cancel()
		<-done
	}
}
