package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/audit"
	"github.com/ervisio/ervisio/server/internal/bridge"
)

const xferManifest = `{"id":"xfer","name":"Xfer","version":"1.0.0","entry":"index.js","platforms":["linux","windows"],
 "capabilities":{"commands":[{"name":"seq","argv":%SEQ%,"args":[{"pattern":"[0-9]{1,6}"}],"admin":false}],
  "http":[{"name":"svc","socket":"%SOCK%","admin":false,"headers":["X-Registry-Auth","Content-Type"],
    "rules":[{"methods":["GET"],"path":"/dl/[a-z0-9]+"},{"methods":["POST","PUT"],"path":"/up"},{"methods":["POST"],"path":"/act"},{"methods":["POST"],"path":"/build"}],
    "maxBody":4096,"maxUpload":2000000,"timeoutSec":5},
   {"name":"inline","socket":"%SOCK%","admin":false,"headers":[],"rules":[{"methods":["POST"],"path":"/len"}],"maxBody":8388608,"timeoutSec":10}],
  "files":{"read":[],"write":[]},"sockets":[],"network":[]},
 "contributes":{"pages":[{"id":"main","title":"Xfer"}],"widgets":[],"snippets":[]},
 "visibleTo":{"groups":[]}}`

// seqArgv is the manifest argv of the "seq" command: Unix seq 1 N, or the cmd.exe loop that prints the same lines.
func seqArgv() string {
	if runtime.GOOS == "windows" {
		return `["cmd","/c","for /l %i in (1,1,{0}) do @echo %i"]`
	}
	return `["seq","1","{0}"]`
}

type xferEnv struct {
	ts     *httptest.Server
	srv    *Server
	cl     *http.Client
	cancel atomic.Int32 // downloads the service saw cancelled
}

func newXferEnv(t *testing.T) *xferEnv {
	t.Helper()
	e := &xferEnv{}
	sockDir, err := os.MkdirTemp("", "xf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/dl/{n}", func(w http.ResponseWriter, r *http.Request) {
		var n int
		fmt.Sscanf(r.PathValue("n"), "%d", &n)
		if r.PathValue("n") == "gone" {
			http.Error(w, `{"message":"No such image: x"}`, http.StatusNotFound)
			return
		}
		if r.PathValue("n") != "chunked" {
			w.Header().Set("Content-Length", fmt.Sprint(n))
		} else {
			n = 3 << 20
		}
		chunk := bytes.Repeat([]byte("0123456789abcdef"), 4096)
		for n > 0 {
			k := min(n, len(chunk))
			if _, err := w.Write(chunk[:k]); err != nil {
				e.cancel.Add(1)
				return
			}
			n -= k
		}
	})
	mux.HandleFunc("/up", func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		fmt.Fprintf(w, `{"n":%d,"auth":%q}`, n, r.Header.Get("X-Registry-Auth"))
	})
	mux.HandleFunc("/len", func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		fmt.Fprintf(w, `{"n":%d}`, n)
	})
	mux.HandleFunc("/build", func(w http.ResponseWriter, r *http.Request) {
		// Reads the whole context, then streams progress lines back.
		n, _ := io.Copy(io.Discard, r.Body)
		fl := w.(http.Flusher)
		for i := 1; i <= 3; i++ {
			fmt.Fprintf(w, "{\"stream\":\"Step %d/3 (%d bytes)\"}\n", i, n)
			fl.Flush()
			time.Sleep(20 * time.Millisecond)
		}
	})
	mux.HandleFunc("/act", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusConflict) })
	hs := &http.Server{Handler: mux}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })

	plugins := t.TempDir()
	p := filepath.Join(plugins, "xfer")
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "manifest.json"), []byte(strings.ReplaceAll(strings.ReplaceAll(xferManifest, "%SEQ%", seqArgv()), "%SOCK%", strings.ReplaceAll(sock, `\`, `\\`))), 0o644)
	os.WriteFile(filepath.Join(p, "index.js"), []byte("export default () => {}"), 0o644)
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html>app"), 0o644)
	srv, err := New(Options{
		ConfigPath:    filepath.Join(t.TempDir(), "ervisio.conf"),
		Dev:           true,
		NoAuth:        true,
		WebDir:        web,
		Bridge:        buildBridge(t),
		PluginDirs:    []string{plugins},
		DevPluginsDir: plugins,
		StateDir:      t.TempDir(),
		Logger:        log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.srv = srv
	e.ts = httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		e.ts.Close()
		for _, s := range srv.sessions.all() {
			srv.sessions.remove(s)
		}
	})
	e.cl = noAuthClient(t, srv, e.ts)
	return e
}

func (e *xferEnv) post(t *testing.T, cl *http.Client, path string, body any, csrf bool) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", e.ts.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if csrf {
		req.Header.Set("X-Requested-With", "ervisio")
	}
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// start asks for a transfer and returns its result (or the error body).
func (e *xferEnv) start(t *testing.T, req map[string]any) (int, map[string]any) {
	t.Helper()
	req["plugin"] = "xfer"
	st, out := e.post(t, e.cl, "/api/plugins/transfer", req, true)
	if r, ok := out["result"].(map[string]any); ok {
		return st, r
	}
	return st, out
}

func errCode(out map[string]any) string {
	if m, ok := out["error"].(map[string]any); ok {
		s, _ := m["code"].(string)
		return s
	}
	return ""
}

func (e *xferEnv) entries(t *testing.T, q audit.Query) []audit.Entry {
	t.Helper()
	// The entry of a transfer is written when its handler returns.
	time.Sleep(100 * time.Millisecond)
	q.Limit = 1000
	l, _, err := e.srv.audit.List(q)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestPluginDownload(t *testing.T) {
	e := newXferEnv(t)
	const n = 5<<20 + 123
	st, r := e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/5243003", "filename": `../we"ird;name.tar`})
	if st != 200 || r["url"] == nil {
		t.Fatalf("start: %d %v", st, r)
	}
	if r["size"] != float64(n) || r["filename"] != "we_ird_name.tar" {
		t.Fatalf("answer %v", r)
	}
	url := e.ts.URL + r["url"].(string)
	resp, err := e.cl.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || got != n {
		t.Fatalf("download: %d, %d bytes", resp.StatusCode, got)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="we_ird_name.tar"`) || resp.Header.Get("Content-Length") != fmt.Sprint(n) || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("headers: %v", resp.Header)
	}
	// Single use.
	resp, _ = e.cl.Get(url)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second use: %d", resp.StatusCode)
	}
	// Audited as a read entry.
	l := e.entries(t, audit.Query{Action: "download"})
	if len(l) != 1 || l[0].Plugin != "xfer" || l[0].Target != "GET /dl/5243003" || l[0].Result != audit.OK || l[0].Bytes == nil || *l[0].Bytes != n || l[0].User == "" || l[0].Via != "svc" {
		t.Fatalf("audit: %+v", l)
	}
}

func TestPluginDownloadCommandAndChunked(t *testing.T) {
	e := newXferEnv(t)
	st, r := e.start(t, map[string]any{"kind": "download", "command": "seq", "args": []string{"1000"}, "filename": "n.txt"})
	if st != 200 || r["size"] != nil {
		t.Fatalf("start: %d %v", st, r)
	}
	resp, _ := e.cl.Get(e.ts.URL + r["url"].(string))
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	b = []byte(strings.ReplaceAll(string(b), "\r\n", "\n")) // cmd.exe ends lines with CRLF
	if !strings.HasPrefix(string(b), "1\n2\n3\n") || !strings.HasSuffix(string(b), "999\n1000\n") {
		t.Fatalf("command output: %d bytes", len(b))
	}
	// Without Content-Length the header is left out.
	st, r = e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/chunked", "filename": "c.bin"})
	if st != 200 {
		t.Fatalf("%d %v", st, r)
	}
	resp, _ = e.cl.Get(e.ts.URL + r["url"].(string))
	got, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if got != 3<<20 {
		t.Fatalf("%d bytes", got)
	}
	l := e.entries(t, audit.Query{Action: "download"})
	if len(l) != 2 || l[0].Action != "download" {
		t.Fatalf("%+v", l)
	}
}

func TestPluginTransferRulesAndErrors(t *testing.T) {
	e := newXferEnv(t)
	for name, req := range map[string]map[string]any{
		"download POST":   {"kind": "download", "name": "svc", "method": "POST", "path": "/act"},
		"download path":   {"kind": "download", "name": "svc", "method": "GET", "path": "/up"},
		"download header": {"kind": "download", "name": "svc", "method": "GET", "path": "/dl/1", "headers": map[string]string{"Cookie": "x"}},
		"unknown api":     {"kind": "download", "name": "nope", "method": "GET", "path": "/dl/1"},
		"unknown cmd":     {"kind": "download", "command": "rm", "args": []string{}},
		"bad cmd arg":     {"kind": "download", "command": "seq", "args": []string{"1; x"}},
		"upload GET":      {"kind": "upload", "name": "svc", "method": "GET", "path": "/dl/1", "size": 1},
		"upload DELETE":   {"kind": "upload", "name": "svc", "method": "DELETE", "path": "/up", "size": 1},
		"upload path":     {"kind": "upload", "name": "svc", "method": "POST", "path": "/dl/1", "size": 1},
		"upload too big":  {"kind": "upload", "name": "svc", "method": "POST", "path": "/up", "size": 2000001},
		"no kind":         {"name": "svc", "method": "GET", "path": "/dl/1"},
	} {
		st, out := e.start(t, req)
		if st < 400 || st >= 500 || errCode(out) == "" {
			t.Errorf("%s: %d %v", name, st, out)
		}
	}
	// A service answer that is not a file is returned to the plugin with its message.
	st, out := e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/gone"})
	m, _ := out["error"].(map[string]any)
	if st != 503 || m == nil || !strings.Contains(fmt.Sprint(m["message"]), "404: No such image: x") {
		t.Fatalf("service error: %d %v", st, out)
	}
	// No CSRF header: refused. A GET cannot start a transfer.
	if st, _ := e.post(t, e.cl, "/api/plugins/transfer", map[string]any{"kind": "download", "plugin": "xfer", "name": "svc", "method": "GET", "path": "/dl/1"}, false); st != 403 {
		t.Fatalf("no csrf: %d", st)
	}
	resp, _ := e.cl.Get(e.ts.URL + "/api/plugins/transfer")
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatal("GET started a transfer")
	}
	// Without a session: 401, for the start and for a link.
	st, _ = e.post(t, http.DefaultClient, "/api/plugins/transfer", map[string]any{"kind": "download"}, true)
	if st != 401 {
		t.Fatalf("anonymous start: %d", st)
	}
	_, r := e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/10", "filename": "x"})
	resp, _ = http.Get(e.ts.URL + r["url"].(string))
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous link: %d", resp.StatusCode)
	}
	// ...and the link still works for its owner (the anonymous try did not use it up).
	resp, _ = e.cl.Get(e.ts.URL + r["url"].(string))
	got, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || got != 10 {
		t.Fatalf("owner after anonymous try: %d %d", resp.StatusCode, got)
	}
	// Denied calls are in the log.
	l := e.entries(t, audit.Query{Action: "download"})
	denied := 0
	for _, x := range l {
		if x.Result == audit.Denied || x.Result == audit.Error {
			denied++
		}
	}
	if denied < 5 {
		t.Fatalf("refused requests logged: %d of %d", denied, len(l))
	}
}

func TestPluginTransferBoundToSession(t *testing.T) {
	e := newXferEnv(t)
	_, r := e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/100", "filename": "x"})
	url := e.ts.URL + r["url"].(string)
	// Another signed-in session may not use it, and does not use it up.
	other := noAuthClient(t, e.srv, e.ts)
	resp, _ := other.Get(url)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other session: %d", resp.StatusCode)
	}
	resp, _ = e.cl.Get(url)
	got, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || got != 100 {
		t.Fatalf("owner: %d %d", resp.StatusCode, got)
	}
}

func TestPluginTransferExpires(t *testing.T) {
	e := newXferEnv(t)
	e.srv.transfers.ttl = 150 * time.Millisecond
	_, r := e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/chunked", "filename": "x"})
	time.Sleep(500 * time.Millisecond)
	resp, _ := e.cl.Get(e.ts.URL + r["url"].(string))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expired link: %d", resp.StatusCode)
	}
	// The slot and the service connection were given back.
	e.srv.transfers.mu.Lock()
	live := e.srv.transfers.live[e.srv.sessions.all()[0]]
	e.srv.transfers.mu.Unlock()
	if live != 0 {
		t.Fatalf("%d transfers still held", live)
	}
	deadline := time.Now().Add(3 * time.Second)
	for e.cancel.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if e.cancel.Load() == 0 {
		t.Fatal("the service connection was not closed")
	}
}

func TestPluginTransferRateLimit(t *testing.T) {
	e := newXferEnv(t)
	limited := 0
	for i := 0; i < maxTransferIssues+5; i++ {
		st, out := e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/nope"})
		if st == 503 && strings.Contains(fmt.Sprint(out), "Too many transfers") {
			limited++
		}
	}
	if limited != 5 {
		t.Fatalf("%d requests rate limited, want 5", limited)
	}
	// Waiting transfers are bounded too.
	e2 := newXferEnv(t)
	for i := 0; i < maxTransfersPerSession; i++ {
		if st, r := e2.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/chunked", "filename": "x"}); st != 200 {
			t.Fatalf("transfer %d: %d %v", i, st, r)
		}
	}
	if st, out := e2.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/chunked"}); st != 503 || !strings.Contains(fmt.Sprint(out), "are running") {
		t.Fatalf("over the live limit: %d %v", st, out)
	}
}

func (e *xferEnv) upload(t *testing.T, size int, body io.Reader, contentLength int64, extra map[string]any) (int, map[string]any) {
	t.Helper()
	req := map[string]any{"kind": "upload", "name": "svc", "method": "POST", "path": "/up", "size": size, "headers": map[string]string{"X-Registry-Auth": "SECRET-TOKEN-123"}}
	for k, v := range extra {
		req[k] = v
	}
	st, r := e.start(t, req)
	if st != 200 {
		return st, r
	}
	hr, _ := http.NewRequest("POST", e.ts.URL+r["url"].(string), body)
	hr.ContentLength = contentLength
	hr.Header.Set("X-Requested-With", "ervisio")
	resp, err := e.cl.Do(hr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if res, ok := out["result"].(map[string]any); ok {
		return resp.StatusCode, res
	}
	return resp.StatusCode, out
}

func TestPluginUpload(t *testing.T) {
	e := newXferEnv(t)
	body := bytes.Repeat([]byte("x"), 1_500_000) // over maxBody (4096), under maxUpload
	st, r := e.upload(t, len(body), bytes.NewReader(body), int64(len(body)), nil)
	if st != 200 || r["status"] != float64(200) || !strings.Contains(fmt.Sprint(r["body"]), `"n":1500000`) {
		t.Fatalf("upload: %d %v", st, r)
	}
	// The header the plugin sent reached the service, but it is not in the log.
	if !strings.Contains(fmt.Sprint(r["body"]), "SECRET-TOKEN-123") {
		t.Fatalf("header not forwarded: %v", r["body"])
	}
	l := e.entries(t, audit.Query{Action: "upload"})
	if len(l) != 1 || l[0].Target != "POST /up" || l[0].Result != audit.OK || l[0].Bytes == nil || *l[0].Bytes != 1_500_000 || l[0].Code == nil || *l[0].Code != 200 {
		t.Fatalf("audit: %+v", l)
	}
	raw, _ := os.ReadFile(filepath.Join(e.srv.audit.Dir(), "audit-"+time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if bytes.Contains(raw, []byte("SECRET-TOKEN-123")) {
		t.Fatal("a secret header value is in the activity log")
	}
	// The service's own status is a normal result, and counts as failed in the log.
	st, r = e.upload(t, 3, strings.NewReader("abc"), 3, map[string]any{"path": "/act"})
	if st != 200 || r["status"] != float64(409) {
		t.Fatalf("service 409: %d %v", st, r)
	}
	if l := e.entries(t, audit.Query{Action: "upload"}); len(l) != 2 || l[0].Result != audit.Failed {
		t.Fatalf("audit after 409: %+v", l)
	}
	// A body that does not match the announced size is refused.
	st, r = e.upload(t, 10, strings.NewReader("12345"), 5, nil)
	if st != 400 || errCode(r) != "invalid" {
		t.Fatalf("size mismatch: %d %v", st, r)
	}
	// A body longer than announced, sent chunked (unknown length).
	st, r = e.upload(t, 4, io.MultiReader(strings.NewReader("12"), strings.NewReader("345678")), -1, nil)
	if st != 400 {
		t.Fatalf("too long: %d %v", st, r)
	}
	// A single-use link: the second POST is refused.
	_, start := e.start(t, map[string]any{"kind": "upload", "name": "svc", "method": "PUT", "path": "/up", "size": 2})
	for i, want := range []int{200, 404} {
		hr, _ := http.NewRequest("POST", e.ts.URL+start["url"].(string), strings.NewReader("hi"))
		hr.Header.Set("X-Requested-With", "ervisio")
		resp, _ := e.cl.Do(hr)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("use %d: %d", i, resp.StatusCode)
		}
	}
	// No CSRF header on the upload.
	_, start = e.start(t, map[string]any{"kind": "upload", "name": "svc", "method": "PUT", "path": "/up", "size": 2})
	hr, _ := http.NewRequest("POST", e.ts.URL+start["url"].(string), strings.NewReader("hi"))
	resp, _ := e.cl.Do(hr)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("upload without csrf header: %d", resp.StatusCode)
	}
}

// A download streams through the daemon: its memory use does not grow with the size.
func TestPluginDownloadMemoryFlat(t *testing.T) {
	if testing.Short() {
		t.Skip("streams 192 MiB")
	}
	e := newXferEnv(t)
	const n = 192 << 20
	_, r := e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": fmt.Sprintf("/dl/%d", n), "filename": "big"})
	runtime.GC()
	var base, peak uint64
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	base = ms.HeapAlloc
	resp, err := e.cl.Get(e.ts.URL + r["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 256<<10)
	var got int64
	for {
		k, err := resp.Body.Read(buf)
		got += int64(k)
		if got%(8<<20) < int64(k) {
			runtime.ReadMemStats(&ms)
			peak = max(peak, ms.HeapAlloc)
		}
		if err != nil {
			break
		}
	}
	if got != n {
		t.Fatalf("%d bytes", got)
	}
	if grow := int64(peak) - int64(base); grow > 48<<20 {
		t.Fatalf("heap grew by %d MiB while streaming %d MiB", grow>>20, n>>20)
	}
}

func TestSanitizeFilename(t *testing.T) {
	for in, want := range map[string]string{
		"busybox.tar":            "busybox.tar",
		"../../etc/passwd":       "passwd",
		`C:\dir\file.zip`:        "file.zip",
		`a"b;c<d>.tar`:           "a_b_c_d_.tar",
		".hidden":                "hidden",
		"":                       "download",
		"...":                    "download",
		"line\nbreak\r.txt":      "line_break_.txt",
		"naïve résumé.tar":       "naïve résumé.tar",
		"x\u202egnp.exe":         "x_gnp.exe",
		strings.Repeat("a", 300): strings.Repeat("a", 200),
		"trailing. ":             "trailing",
		"100%":                   "100_",
		"$(rm -rf).tar":          "_(rm -rf).tar",
	} {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	cd := contentDisposition("naïve résumé.tar")
	if !strings.Contains(cd, `filename="na_ve r_sum_.tar"`) || !strings.Contains(cd, "filename*=UTF-8''na%C3%AFve%20r%C3%A9sum%C3%A9.tar") {
		t.Fatalf("%s", cd)
	}
	if strings.ContainsAny(contentDisposition("a\r\nSet-Cookie: x"), "\r\n") {
		t.Fatal("header injection")
	}
}

func TestTransferStoreSingleUseAndExpiry(t *testing.T) {
	ts := newTransferStore()
	sess := &Session{}
	if e := ts.reserve(sess); e != nil {
		t.Fatal(e)
	}
	// take is exclusive.
	tr := &transfer{sess: sess, cancel: func() {}, release: func() {}}
	tr.timer = time.NewTimer(time.Hour)
	ts.mu.Lock()
	ts.byID[tokenID("tok")] = tr
	ts.mu.Unlock()
	if ts.take("tok", &Session{}) != nil {
		t.Fatal("another session took the transfer")
	}
	if ts.take("tok", sess) != tr || ts.take("tok", sess) != nil {
		t.Fatal("a token must work once")
	}
	if ts.take("unknown", sess) != nil || ts.take("", sess) != nil {
		t.Fatal("unknown token")
	}
	// Tokens are random and long.
	ts.ttl = time.Hour
	a, _, _ := ts.put(&transfer{sess: sess, cancel: func() {}, release: func() {}})
	b, _, _ := ts.put(&transfer{sess: sess, cancel: func() {}, release: func() {}})
	if a == b || len(a) < 40 {
		t.Fatalf("tokens %q %q", a, b)
	}
}

func TestTransferStoreLimits(t *testing.T) {
	ts := newTransferStore()
	now := time.Now()
	ts.now = func() time.Time { return now }
	sess := &Session{}
	for i := 0; i < maxTransfersPerSession; i++ {
		if e := ts.reserve(sess); e != nil {
			t.Fatalf("reserve %d: %v", i, e)
		}
	}
	if e := ts.reserve(sess); e == nil {
		t.Fatal("live limit")
	}
	for i := 0; i < maxTransfersPerSession; i++ {
		ts.done(sess)
	}
	// Rate: 30 per minute, counted over a sliding minute.
	for i := maxTransfersPerSession; i < maxTransferIssues; i++ {
		if e := ts.reserve(sess); e != nil {
			t.Fatalf("issue %d: %v", i, e)
		}
		ts.done(sess)
	}
	if e := ts.reserve(sess); e == nil {
		t.Fatal("rate limit")
	}
	now = now.Add(61 * time.Second)
	if e := ts.reserve(sess); e != nil {
		t.Fatalf("after a minute: %v", e)
	}
	// Another session is not affected.
	if e := ts.reserve(&Session{}); e != nil {
		t.Fatal(e)
	}
	ts.done(sess)
	now = now.Add(2 * time.Minute)
	ts.gc()
	if len(ts.issued) != 1 {
		t.Fatalf("gc kept %d sessions", len(ts.issued))
	}
}

// Administrators see every entry, others their own; a plugin only its own.
func TestAuditQueryScope(t *testing.T) {
	s := originServer(false, nil)
	s.audit = audit.New(t.TempDir(), configAudit{s.cfg})
	user := &Session{Account: &account.Account{Name: "bob", UID: 1001}}
	root := &Session{Account: &account.Account{Name: "root", UID: 0}}
	if runtime.GOOS == "windows" {
		// No uid 0 here: an administrator is a session with a live root
		// bridge (unlocked). A user bridge stands in for it.
		me, err := account.Current()
		if err != nil {
			t.Fatal(err)
		}
		p, err := bridge.StartUser(context.Background(), &bridge.Spec{Bridge: buildBridge(t), Config: filepath.Join(t.TempDir(), "c.conf"), Account: me, Logger: log.New(io.Discard, "", 0)})
		if err != nil {
			t.Fatal(err)
		}
		defer p.Stop()
		root.mu.Lock()
		root.root, root.rootUsed = p, time.Now()
		root.mu.Unlock()
	}
	q, e := s.auditQuery(user, auditParams{User: "alice", Plugin: "docker"}, "")
	if e != nil || q.User != "bob" || q.Plugin != "docker" {
		t.Fatalf("user asking for another user: %+v %v", q, e)
	}
	q, _ = s.auditQuery(root, auditParams{User: "alice"}, "")
	if q.User != "alice" {
		t.Fatalf("admin: %+v", q)
	}
	q, _ = s.auditQuery(root, auditParams{Plugin: "other"}, "docker")
	if q.Plugin != "docker" || q.Source != audit.SourcePlugin {
		t.Fatalf("plugin scope: %+v", q)
	}
	if _, e := s.auditQuery(root, auditParams{Since: "yesterday"}, ""); e == nil {
		t.Fatal("bad time accepted")
	}
	q, _ = s.auditQuery(root, auditParams{Since: "2026-10-01T00:00:00Z", Until: float64(1790000000000)}, "")
	if q.Since.IsZero() || q.Until.IsZero() {
		t.Fatalf("times: %+v", q)
	}
}

// plugins.upload with onResponseChunk: the response comes back as ndjson while the file goes up.
func TestPluginUploadStreamedResponse(t *testing.T) {
	e := newXferEnv(t)
	ctxBody := bytes.Repeat([]byte("c"), 900_000)
	st, r := e.start(t, map[string]any{"kind": "upload", "name": "svc", "method": "POST", "path": "/build", "size": len(ctxBody), "stream": true})
	if st != 200 {
		t.Fatalf("start: %d %v", st, r)
	}
	hr, _ := http.NewRequest("POST", e.ts.URL+r["url"].(string), bytes.NewReader(ctxBody))
	hr.Header.Set("X-Requested-With", "ervisio")
	resp, err := e.cl.Do(hr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("%d %v", resp.StatusCode, resp.Header)
	}
	raw, _ := io.ReadAll(resp.Body)
	var text strings.Builder
	var sawStart, sawDone bool
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var m struct {
			Start *struct {
				Status int `json:"status"`
			} `json:"start"`
			Data  []byte `json:"data"`
			Done  bool   `json:"done"`
			Error any    `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &m); err != nil || m.Error != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		if m.Start != nil && m.Start.Status == 200 {
			sawStart = true
		}
		text.Write(m.Data)
		sawDone = sawDone || m.Done
	}
	if !sawStart || !sawDone || !strings.Contains(text.String(), "Step 3/3 (900000 bytes)") {
		t.Fatalf("start %v done %v text %q", sawStart, sawDone, text.String())
	}
	l := e.entries(t, audit.Query{Action: "upload"})
	if len(l) != 1 || l[0].Result != audit.OK || l[0].Bytes == nil || *l[0].Bytes != 900_000 || l[0].Target != "POST /build" {
		t.Fatalf("audit: %+v", l)
	}
	// A short body is reported before any response starts, as a normal error.
	_, r = e.start(t, map[string]any{"kind": "upload", "name": "svc", "method": "POST", "path": "/build", "size": 100, "stream": true})
	hr, _ = http.NewRequest("POST", e.ts.URL+r["url"].(string), strings.NewReader("short"))
	hr.Header.Set("X-Requested-With", "ervisio")
	resp2, _ := e.cl.Do(hr)
	b2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode < 400 && !strings.Contains(string(b2), `"error"`) {
		t.Fatalf("short streamed upload: %d %s", resp2.StatusCode, b2)
	}
}

// Request bodies up to the default maxBody (8 MiB) reach plugins.http through /api/rpc and plugins.httpStream
// through the WebSocket; a body over the transport limit gets a clear error.
func TestPluginInlineBodyLimits(t *testing.T) {
	e := newXferEnv(t)
	body := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("b"), 3<<20))
	r := rpcOK(t, e, "plugins.http", map[string]any{"plugin": "xfer", "name": "inline", "method": "POST", "path": "/len", "body": body, "b64": true})
	if !strings.Contains(fmt.Sprint(r["body"]), "3145728") {
		t.Fatalf("3 MiB through /api/rpc: %v", r)
	}
	// The same through a stream, in one WebSocket frame.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.ts.URL, "http")+"/api/ws", &websocket.DialOptions{HTTPHeader: cookieHeader(e.cl, e.ts.URL)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	open, _ := json.Marshal(map[string]any{"ch": 1, "op": "open", "method": "plugins.httpStream", "params": map[string]any{"plugin": "xfer", "name": "inline", "method": "POST", "path": "/len", "body": body, "b64": true}})
	if err := c.Write(ctx, websocket.MessageText, open); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("stream: %v (%s)", err, got.String())
		}
		var f wsFrame
		json.Unmarshal(data, &f)
		if f.Op == "error" {
			t.Fatalf("%s", data)
		}
		if f.Op == "data" && f.B64 {
			var s string
			json.Unmarshal(f.Data, &s)
			b, _ := base64.StdEncoding.DecodeString(s)
			got.Write(b)
		}
		if f.Op == "end" {
			break
		}
	}
	if !strings.Contains(got.String(), "3145728") {
		t.Fatalf("3 MiB through the WebSocket: %q", got.String())
	}
	// Over the limit: an error that names the way out, not a dropped connection.
	huge := strings.Repeat("A", MaxRPCBody)
	b, _ := json.Marshal(map[string]any{"method": "plugins.http", "params": map[string]any{"plugin": "xfer", "name": "inline", "method": "POST", "path": "/len", "body": huge, "b64": true}})
	code, out := doc(t, e.cl, "POST", e.ts.URL+"/api/rpc", string(b), true)
	if code != 400 || !strings.Contains(out, "plugins.upload") {
		t.Fatalf("oversize rpc: %d %s", code, out)
	}
}

// status asks GET .../status for a transfer link, as a client.
func (e *xferEnv) status(t *testing.T, cl *http.Client, link string, wait int) (int, map[string]any) {
	t.Helper()
	resp, err := cl.Get(fmt.Sprintf("%s%s/status?wait=%d", e.ts.URL, link, wait))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Result map[string]any `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Result
}

func TestDownloadStatusReportsTheEnd(t *testing.T) {
	e := newXferEnv(t)
	const n = 3<<20 + 7
	_, r := e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": fmt.Sprintf("/dl/%d", n), "filename": "x"})
	link := r["url"].(string)
	// Not fetched yet: a short wait answers "not done".
	if st, res := e.status(t, e.cl, link, 1); st != 200 || res["done"] != false {
		t.Fatalf("before the fetch: %d %v", st, res)
	}
	// Another session cannot read it, or learn that it exists.
	if st, _ := e.status(t, noAuthClient(t, e.srv, e.ts), link, 0); st != http.StatusNotFound {
		t.Fatalf("other session: %d", st)
	}
	// A waiting poll is answered when the download ends.
	got := make(chan map[string]any, 1)
	go func() { _, res := e.status(t, e.cl, link, 20); got <- res }()
	time.Sleep(100 * time.Millisecond)
	resp, err := e.cl.Get(e.ts.URL + link)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	select {
	case res := <-got:
		if res["done"] != true || res["ok"] != true || res["bytes"] != float64(n) || res["error"] != nil {
			t.Fatalf("result %v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting poll was not answered")
	}
	// It stays readable after the end.
	if _, res := e.status(t, e.cl, link, 0); res["ok"] != true || res["bytes"] != float64(n) {
		t.Fatalf("after: %v", res)
	}
	if st, _ := e.status(t, e.cl, "/api/plugins/transfer/nope", 0); st != http.StatusNotFound {
		t.Fatalf("unknown token: %d", st)
	}
}

func TestDownloadStatusReportsFailures(t *testing.T) {
	e := newXferEnv(t)
	// Cut short by the browser.
	_, r := e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/chunked", "filename": "x"})
	link := r["url"].(string)
	resp, err := e.cl.Get(e.ts.URL + link)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Read(make([]byte, 10))
	resp.Body.Close()
	_, res := e.status(t, e.cl, link, 10)
	if res["done"] != true || res["ok"] != false || res["error"] == nil {
		t.Fatalf("cut short: %v", res)
	}
	// Never fetched: the link expires.
	e.srv.transfers.ttl = 100 * time.Millisecond
	_, r = e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/100", "filename": "x"})
	_, res = e.status(t, e.cl, r["url"].(string), 10)
	if res["done"] != true || res["ok"] != false || !strings.Contains(fmt.Sprint(res["error"]), "did not start") {
		t.Fatalf("expired: %v", res)
	}
	// An upload has no status.
	_, r = e.start(t, map[string]any{"kind": "upload", "name": "svc", "method": "POST", "path": "/up", "size": 3})
	if st, _ := e.status(t, e.cl, r["url"].(string), 0); st != http.StatusNotFound {
		t.Fatalf("upload status: %d", st)
	}
}
