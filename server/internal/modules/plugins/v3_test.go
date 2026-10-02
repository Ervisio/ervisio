package plugins

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// SDK v3: capabilities.http, pty commands, admin folders, mkdir/remove.

const v3Manifest = `{
  "id": "v3", "name": "V3 demo", "version": "1.0.0", "entry": "index.js",
  "capabilities": {
    "commands": [
      {"name": "cat", "pty": true, "argv": ["cat"], "admin": false},
      {"name": "shell", "pty": true, "argv": ["sh", "-c", "{0}"], "args": [{"pattern": "echo [a-z]+; exit [0-9]"}], "admin": false},
      {"name": "plain", "argv": ["echo", "plain"], "admin": false}
    ],
    "http": [
      {"name": "api", "socket": "%SOCK%", "admin": false,
       "headers": ["Content-Type", "X-Registry-Auth"],
       "rules": [
         {"methods": ["GET"], "path": "/v1\\.[0-9]+/containers/json"},
         {"methods": ["GET", "POST"], "path": "/v1\\.[0-9]+/containers/[a-zA-Z0-9_.-]+/(start|logs)"},
         {"methods": ["GET"], "path": "/(redirect|big|bin|echo|stream|slow)"}
       ],
       "maxBody": 1024, "timeoutSec": 2},
      {"name": "root-api", "socket": "%SOCK%", "admin": true, "rules": [{"methods": ["GET"], "path": "/echo"}]},
      {"name": "group-api", "socket": "%SOCK%", "admin": true, "adminUnlessGroup": "%GROUP%", "rules": [{"methods": ["GET"], "path": "/echo"}]}
    ],
    "files": {
      "read": ["%RO%", {"path": "%ADMIN%", "admin": true}],
      "write": [%RW%, {"path": "%ADMIN%", "admin": true}, {"path": "%GADMIN%", "admin": true, "adminUnlessGroup": "%GROUP%"}, {"path": "%AUTO%", "create": true}]
    },
    "sockets": [], "network": []
  },
  "contributes": {"pages": [], "widgets": [], "snippets": []},
  "visibleTo": {"groups": []}
}`

type v3env struct {
	sock, ro, rw, admin, gadmin, auto string
	group                             string
	srv                               *http.Server
	lastReq                           chan *http.Request
	streamDone                        chan struct{}
}

// myGroup is a group the test user is in (for adminUnlessGroup).
func myGroup(t *testing.T) string {
	for g := range currentCaller(false).Groups {
		if groupRe.MatchString(g) {
			return g
		}
	}
	t.Skip("the test user has no usable group")
	return ""
}

func setupV3(t *testing.T) *v3env {
	t.Helper()
	system, _ := setup(t)
	base := t.TempDir()
	e := &v3env{
		sock:       filepath.Join(base, "api.sock"),
		ro:         filepath.Join(base, "ro"),
		rw:         filepath.Join(base, "rw"),
		admin:      filepath.Join(base, "admin"),
		gadmin:     filepath.Join(base, "gadmin"),
		auto:       filepath.Join(base, "auto", "deep"),
		group:      myGroup(t),
		lastReq:    make(chan *http.Request, 16),
		streamDone: make(chan struct{}, 1),
	}
	for _, d := range []string{e.ro, e.rw, e.admin, e.gadmin} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(e.admin, "a.txt"), []byte("admin"), 0o644)
	r := strings.NewReplacer("%SOCK%", e.sock, "%RO%", e.ro, "%RW%", `"`+e.rw+`"`, "%ADMIN%", e.admin, "%GADMIN%", e.gadmin, "%AUTO%", e.auto, "%GROUP%", e.group)
	writePlugin(t, filepath.Join(system, "v3"), r.Replace(v3Manifest), map[string]string{"index.js": "x"})

	ln, err := net.Listen("unix", e.sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		select {
		case e.lastReq <- r:
		default:
		}
		switch {
		case r.URL.Path == "/redirect":
			http.Redirect(w, r, "/echo", http.StatusFound)
		case r.URL.Path == "/big":
			w.Write(bytes.Repeat([]byte("x"), 2048))
		case r.URL.Path == "/bin":
			w.Write([]byte{0xff, 0x00, 0xfe})
		case r.URL.Path == "/slow":
			time.Sleep(3 * time.Second)
		case r.URL.Path == "/stream":
			w.Header().Set("X-Kind", "stream")
			w.Header().Add("Set-Cookie", "a=b")
			fl := w.(http.Flusher)
			for i := 0; ; i++ {
				if _, err := fmt.Fprintf(w, "chunk%d\n", i); err != nil {
					break
				}
				fl.Flush()
				select {
				case <-r.Context().Done():
					e.streamDone <- struct{}{}
					return
				case <-time.After(20 * time.Millisecond):
				}
			}
		default:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"method": r.Method, "path": r.URL.Path, "query": r.URL.RawQuery, "host": r.Host,
				"ct": r.Header.Get("Content-Type"), "auth": r.Header.Get("X-Registry-Auth"), "body": string(body),
			})
		}
	})
	e.srv = &http.Server{Handler: mux}
	go e.srv.Serve(ln)
	t.Cleanup(func() { e.srv.Close() })
	return e
}

func TestManifestV3Valid(t *testing.T) {
	r := strings.NewReplacer("%SOCK%", "/run/x.sock", "%RO%", "/srv", "%RW%", `"~/rw"`, "%ADMIN%", "/opt/stacks", "%GADMIN%", "/opt/g", "%AUTO%", "~/.config/x", "%GROUP%", "docker")
	m, err := ParseManifest([]byte(r.Replace(v3Manifest)))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Capabilities.HTTP) != 3 || m.Capabilities.HTTP[0].Rules[0].re == nil || !m.RunsRoot() {
		t.Fatalf("%+v", m.Capabilities.HTTP)
	}
	if !m.Capabilities.Commands[0].PTY || m.Capabilities.Files.Write[3].Create != true {
		t.Fatalf("%+v", m.Capabilities)
	}
	// Plain folders marshal back as strings; objects as objects.
	b, _ := json.Marshal(m.Capabilities.Files)
	if !strings.Contains(string(b), `"read":["/srv",{"path":"/opt/stacks","admin":true}]`) {
		t.Fatalf("files marshal: %s", b)
	}
	// A v2 manifest keeps its exact capability shape.
	v2, _ := ParseManifest([]byte(goodManifest))
	b, _ = json.Marshal(v2.Capabilities.Files)
	if string(b) != `{"read":["/srv","~/projects"],"write":[]}` {
		t.Fatalf("v2 files: %s", b)
	}
	if v2.Capabilities.HTTP == nil {
		t.Fatal("http must normalise to []")
	}
	// Consent round trip (plugins.install compares JSON).
	var back Capabilities
	raw, _ := json.Marshal(m.Capabilities)
	if err := json.Unmarshal(raw, &back); err != nil || !sameJSON(emptyIfNil(back), m.Capabilities) {
		t.Fatalf("consent round trip: %v", err)
	}
}

func TestManifestV3Invalid(t *testing.T) {
	base := `{"id":"xx","name":"X","version":"1.0.0","entry":"index.js","capabilities":{%s},"contributes":{},"visibleTo":{}}`
	api := func(fields string) string {
		return fmt.Sprintf(base, `"http":[{"name":"api","socket":"/run/a.sock","rules":[{"methods":["GET"],"path":"/x"}]`+fields+`}]`)
	}
	good := api("")
	if _, err := ParseManifest([]byte(good)); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	bad := map[string]string{
		"name upper":         strings.Replace(good, `"name":"api"`, `"name":"Api"`, 1),
		"name long":          strings.Replace(good, `"name":"api"`, `"name":"a`+strings.Repeat("b", 32)+`"`, 1),
		"relative socket":    strings.Replace(good, `/run/a.sock`, `run/a.sock`, 1),
		"unclean socket":     strings.Replace(good, `/run/a.sock`, `/run/../a.sock`, 1),
		"long socket":        strings.Replace(good, `/run/a.sock`, "/run/"+strings.Repeat("a", 103), 1),
		"lower method":       strings.Replace(good, `["GET"]`, `["get"]`, 1),
		"odd method":         strings.Replace(good, `["GET"]`, `["CONNECT"]`, 1),
		"no methods":         strings.Replace(good, `["GET"]`, `[]`, 1),
		"bad regexp":         strings.Replace(good, `"path":"/x"`, `"path":"/x("`, 1),
		"no rules":           strings.Replace(good, `"rules":[{"methods":["GET"],"path":"/x"}]`, `"rules":[]`, 1),
		"unknown field":      api(`,"proxy":true`),
		"host header":        api(`,"headers":["Host"]`),
		"cookie header":      api(`,"headers":["cookie"]`),
		"authorization":      api(`,"headers":["Authorization"]`),
		"connection":         api(`,"headers":["Connection"]`),
		"upgrade":            api(`,"headers":["Upgrade"]`),
		"transfer-encoding":  api(`,"headers":["Transfer-Encoding"]`),
		"content-length":     api(`,"headers":["Content-Length"]`),
		"proxy-*":            api(`,"headers":["Proxy-Foo"]`),
		"sec-*":              api(`,"headers":["Sec-Fetch-Site"]`),
		"header syntax":      api(`,"headers":["X Bad"]`),
		"header twice":       api(`,"headers":["X-A","x-a"]`),
		"maxBody":            api(`,"maxBody":67108865`),
		"timeout":            api(`,"timeoutSec":601`),
		"group without adm":  api(`,"adminUnlessGroup":"docker"`),
		"duplicate name":     fmt.Sprintf(base, `"http":[{"name":"a","socket":"/s","rules":[{"methods":["GET"],"path":"/"}]},{"name":"a","socket":"/s","rules":[{"methods":["GET"],"path":"/"}]}]`),
		"folder unknown":     fmt.Sprintf(base, `"files":{"read":[{"path":"/srv","recursive":true}]}`),
		"folder no path":     fmt.Sprintf(base, `"files":{"read":[{"admin":true}]}`),
		"admin under home":   fmt.Sprintf(base, `"files":{"write":[{"path":"~/x","admin":true}]}`),
		"folder group alone": fmt.Sprintf(base, `"files":{"write":[{"path":"/x","adminUnlessGroup":"docker"}]}`),
		"folder number":      fmt.Sprintf(base, `"files":{"read":[1]}`),
	}
	for what, m := range bad {
		if _, err := ParseManifest([]byte(m)); err == nil {
			t.Errorf("%s: accepted", what)
		}
	}
}

func TestCleanHTTPPath(t *testing.T) {
	for raw, want := range map[string]string{
		"/v1.41/containers/json": "/v1.41/containers/json",
		"/":                      "/",
		"/a%20b":                 "/a b",
		"/images/x%3Alatest":     "/images/x:latest",
	} {
		if got, ok := cleanHTTPPath(raw); !ok || got != want {
			t.Errorf("%q: %q %v", raw, got, ok)
		}
	}
	for _, bad := range []string{
		"", "v1/x", "//host/x", "http://host/x", "/a/../b", "/a/./b", "/a//b", "/a/", "/..", "/a/..",
		"/a%2fb", "/a%2Fb", "/%2e%2e", "/a/%2E/b", "/a%5cb", "/a%00", "/a\\b", "/a\r\nX: y", "/a%0d%0a",
		"/a b", "/a?x=1", "/a#x", "/a%zz", "/a%c3", "/" + strings.Repeat("a", maxHTTPPath),
	} {
		if got, ok := cleanHTTPPath(bad); ok {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
	for _, q := range []string{"all=1&filters=%7B%7D", "", "a=b+c"} {
		if !validQuery(q) {
			t.Errorf("query %q refused", q)
		}
	}
	for _, q := range []string{"a=b c", "a=\r\n", "a#b", "a=\x00", strings.Repeat("a", maxHTTPQuery+1)} {
		if validQuery(q) {
			t.Errorf("query %q accepted", q)
		}
	}
}

func TestHTTPRules(t *testing.T) {
	e := setupV3(t)
	try := func(p HTTPParams) error {
		p.Plugin = "v3"
		if p.Name == "" {
			p.Name = "api"
		}
		_, err := resolveHTTP(context.Background(), &rpc.Call{}, p)
		return err
	}
	for _, ok := range []HTTPParams{
		{Method: "GET", Path: "/v1.41/containers/json", Query: "all=1"},
		{Method: "POST", Path: "/v1.41/containers/abc_1/start"},
		{Method: "GET", Path: "/v1.41/containers/abc/logs", Headers: map[string]string{"x-registry-auth": "e30="}},
	} {
		if err := try(ok); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	for what, bad := range map[string]HTTPParams{
		"method not in rule":   {Method: "DELETE", Path: "/v1.41/containers/json"},
		"POST on GET rule":     {Method: "POST", Path: "/v1.41/containers/json"},
		"lower-case method":    {Method: "get", Path: "/v1.41/containers/json"},
		"anchored end":         {Method: "GET", Path: "/v1.41/containers/json/x"},
		"anchored start":       {Method: "GET", Path: "/x/v1.41/containers/json"},
		"traversal":            {Method: "POST", Path: "/v1.41/containers/abc/../../secrets/start"},
		"encoded slash":        {Method: "POST", Path: "/v1.41/containers/a%2f..%2fb/start"},
		"encoded dots":         {Method: "POST", Path: "/v1.41/containers/%2e%2e/start"},
		"query in path":        {Method: "GET", Path: "/v1.41/containers/json?all=1"},
		"absolute form":        {Method: "GET", Path: "http://evil/v1.41/containers/json"},
		"crlf query":           {Method: "GET", Path: "/v1.41/containers/json", Query: "a=1\r\nHost: x"},
		"undeclared header":    {Method: "GET", Path: "/v1.41/containers/json", Headers: map[string]string{"X-Other": "1"}},
		"forbidden header":     {Method: "GET", Path: "/v1.41/containers/json", Headers: map[string]string{"Host": "evil"}},
		"crlf header value":    {Method: "GET", Path: "/v1.41/containers/json", Headers: map[string]string{"Content-Type": "a\r\nX: y"}},
		"body over maxBody":    {Method: "POST", Path: "/v1.41/containers/a/start", Body: strings.Repeat("x", 1025)},
		"bad base64":           {Method: "POST", Path: "/v1.41/containers/a/start", Body: "!!", B64: true},
		"unknown api":          {Name: "nope", Method: "GET", Path: "/echo"},
		"bad api name":         {Name: "../x", Method: "GET", Path: "/echo"},
		"undeclared redirect?": {Method: "GET", Path: "/elsewhere"},
	} {
		if err := try(bad); err == nil {
			t.Errorf("%s: accepted", what)
		}
	}
	if err := try(HTTPParams{Name: "nope", Method: "GET", Path: "/echo"}); !rpc.IsCode(err, rpc.NotFound) {
		t.Errorf("unknown api: %v", err)
	}
	if err := try(HTTPParams{Method: "GET", Path: "/elsewhere"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("undeclared path: %v", err)
	}
	_ = e
}

func TestHTTPAdminGating(t *testing.T) {
	setupV3(t)
	do := func(admin bool, name string) error {
		_, err := runHTTP(context.Background(), &rpc.Call{Admin: admin}, HTTPParams{Plugin: "v3", Name: name, Method: "GET", Path: "/echo"})
		return err
	}
	// A user-level API never runs on the root bridge.
	if err := do(true, "api"); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("user api on the root bridge: %v", err)
	}
	// An admin API runs on the root bridge...
	if err := do(true, "root-api"); err != nil {
		t.Errorf("admin api on the root bridge: %v", err)
	}
	// ...and asks for admin on the user bridge, unless the user is in adminUnlessGroup.
	if os.Geteuid() != 0 {
		if err := do(false, "root-api"); !rpc.IsCode(err, rpc.NeedsAdmin) {
			t.Errorf("admin api on the user bridge: %v", err)
		}
	}
	if err := do(false, "group-api"); err != nil {
		t.Errorf("admin api for a member of adminUnlessGroup: %v", err)
	}
	// Disabled plugin: nothing.
	st := readState()
	st.Enabled["v3"] = false
	writeState(st)
	if err := do(false, "group-api"); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("disabled plugin: %v", err)
	}
}

func TestHTTPCall(t *testing.T) {
	e := setupV3(t)
	do := func(p HTTPParams) (*HTTPResult, error) {
		p.Plugin, p.Name = "v3", "api"
		return runHTTP(context.Background(), &rpc.Call{}, p)
	}
	res, err := do(HTTPParams{Method: "POST", Path: "/v1.41/containers/web/start", Query: "t=5", Body: `{"a":1}`, JSON: true,
		Headers: map[string]string{"X-Registry-Auth": "e30="}})
	if err != nil || res.Status != 200 || res.B64 {
		t.Fatalf("%+v %v", res, err)
	}
	var got map[string]string
	json.Unmarshal([]byte(res.Body), &got)
	want := map[string]string{"method": "POST", "path": "/v1.41/containers/web/start", "query": "t=5", "host": "localhost",
		"ct": "application/json", "auth": "e30=", "body": `{"a":1}`}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if res.Headers["Content-Type"] != "application/json" {
		t.Errorf("headers %v", res.Headers)
	}
	// Redirects are returned, not followed.
	res, err = do(HTTPParams{Method: "GET", Path: "/redirect"})
	if err != nil || res.Status != http.StatusFound {
		t.Fatalf("redirect: %+v %v", res, err)
	}
	// Binary bodies come back as base64.
	res, err = do(HTTPParams{Method: "GET", Path: "/bin"})
	if err != nil || !res.B64 || res.Body != base64.StdEncoding.EncodeToString([]byte{0xff, 0, 0xfe}) {
		t.Fatalf("bin: %+v %v", res, err)
	}
	// The response is capped at maxBody.
	if _, err := do(HTTPParams{Method: "GET", Path: "/big"}); !rpc.IsCode(err, rpc.Unavailable) {
		t.Fatalf("big: %v", err)
	}
	// timeoutSec.
	if _, err := do(HTTPParams{Method: "GET", Path: "/slow"}); !rpc.IsCode(err, rpc.Unavailable) {
		t.Fatalf("slow: %v", err)
	}
	// A missing socket is unavailable.
	e.srv.Close()
	os.Remove(e.sock)
	if _, err := do(HTTPParams{Method: "GET", Path: "/echo"}); !rpc.IsCode(err, rpc.Unavailable) {
		t.Fatalf("no socket: %v", err)
	}
}

// fakeStream records events and lets a test send input.
type fakeStream struct {
	mu     sync.Mutex
	events []json.RawMessage
	data   bytes.Buffer
	in     chan json.RawMessage
	notify chan struct{}
}

func newFakeStream() *fakeStream {
	return &fakeStream{in: make(chan json.RawMessage, 16), notify: make(chan struct{}, 1024)}
}
func (f *fakeStream) Send(v any) error {
	b, _ := json.Marshal(v)
	f.mu.Lock()
	f.events = append(f.events, b)
	f.mu.Unlock()
	f.ping()
	return nil
}
func (f *fakeStream) SendBytes(b []byte) error {
	f.mu.Lock()
	f.data.Write(b)
	f.mu.Unlock()
	f.ping()
	return nil
}
func (f *fakeStream) Input() <-chan json.RawMessage { return f.in }
func (f *fakeStream) ping() {
	select {
	case f.notify <- struct{}{}:
	default:
	}
}
func (f *fakeStream) input(v any) {
	b, _ := json.Marshal(v)
	f.in <- b
}

// waitFor polls cond until it holds or the deadline passes.
func (f *fakeStream) waitFor(t *testing.T, what string, cond func(data string, events []json.RawMessage) bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		f.mu.Lock()
		ok := cond(f.data.String(), f.events)
		f.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-f.notify:
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			f.mu.Lock()
			defer f.mu.Unlock()
			t.Fatalf("timed out waiting for %s; data %q events %s", what, f.data.String(), f.events)
		}
	}
}

func TestHTTPStream(t *testing.T) {
	e := setupV3(t)
	s := newFakeStream()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runHTTPStream(ctx, &rpc.Call{}, s, HTTPParams{Plugin: "v3", Name: "api", Method: "GET", Path: "/stream"})
	}()
	s.waitFor(t, "three chunks", func(d string, ev []json.RawMessage) bool { return strings.Contains(d, "chunk2\n") && len(ev) == 1 })
	var start struct {
		Status  int
		Headers map[string]string
	}
	json.Unmarshal(s.events[0], &start)
	if start.Status != 200 || start.Headers["X-Kind"] != "stream" || start.Headers["Set-Cookie"] != "" {
		t.Fatalf("start event %s", s.events[0])
	}
	// Closing the stream (input closed) ends the request on the server.
	close(s.in)
	select {
	case <-e.streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was not closed")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Rules apply to streams too.
	if err := runHTTPStream(ctx, &rpc.Call{}, newFakeStream(), HTTPParams{Plugin: "v3", Name: "api", Method: "GET", Path: "/v1.41/secrets"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatalf("undeclared stream path: %v", err)
	}
}

func TestPTY(t *testing.T) {
	setupV3(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := func(s *fakeStream, cmd string, args ...string) chan error {
		done := make(chan error, 1)
		go func() {
			done <- runPTY(ctx, &rpc.Call{}, s, PTYParams{ExecParams: ExecParams{Plugin: "v3", Command: cmd, Args: args}, Cols: 80, Rows: 24})
		}()
		return done
	}
	exitCode := func(ev []json.RawMessage) (int, bool) {
		for _, e := range ev {
			var x struct {
				Type string
				Code int
			}
			if json.Unmarshal(e, &x) == nil && x.Type == "exit" {
				return x.Code, true
			}
		}
		return 0, false
	}

	// Input goes in, output comes out, resize is accepted, EOF ends cat.
	s := newFakeStream()
	done := run(s, "cat")
	s.input(map[string]any{"type": "resize", "cols": 100, "rows": 30})
	s.input(map[string]any{"type": "input", "data": base64.StdEncoding.EncodeToString([]byte("hello pty\n"))})
	s.waitFor(t, "echo", func(d string, _ []json.RawMessage) bool { return strings.Count(d, "hello pty") >= 2 })
	s.input(map[string]any{"type": "input", "data": base64.StdEncoding.EncodeToString([]byte{4})})
	s.waitFor(t, "exit", func(_ string, ev []json.RawMessage) bool { _, ok := exitCode(ev); return ok })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if c, _ := exitCode(s.events); c != 0 {
		t.Fatalf("cat exit %d", c)
	}

	// Exit code of a validated argument.
	s = newFakeStream()
	done = run(s, "shell", "echo hi; exit 3")
	s.waitFor(t, "exit", func(_ string, ev []json.RawMessage) bool { _, ok := exitCode(ev); return ok })
	<-done
	if c, _ := exitCode(s.events); c != 3 || !strings.Contains(s.data.String(), "hi") {
		t.Fatalf("exit %d, output %q", c, s.data.String())
	}

	// Closing the stream kills the process.
	s = newFakeStream()
	done = run(s, "cat")
	s.waitFor(t, "start", func(string, []json.RawMessage) bool { return true })
	close(s.in)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing the stream did not end the pty")
	}

	// pty commands only through plugins.pty, and plugins.pty only for them.
	if _, err := runExec(ctx, &rpc.Call{}, ExecParams{Plugin: "v3", Command: "cat"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("pty command through exec: %v", err)
	}
	if err := <-run(newFakeStream(), "plain"); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("plain command through pty: %v", err)
	}
	if err := <-run(newFakeStream(), "shell", "rm -rf /"); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("argument not matching the pattern: %v", err)
	}
	// User-level pty command never on the root bridge.
	err := runPTY(ctx, &rpc.Call{Admin: true}, newFakeStream(), PTYParams{ExecParams: ExecParams{Plugin: "v3", Command: "cat"}})
	if !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("user pty on the root bridge: %v", err)
	}
}

func TestAdminFoldersAndMkdirRemove(t *testing.T) {
	e := setupV3(t)
	read := func(admin bool, path string) error {
		_, err := readPluginFile(context.Background(), call(t, admin, FileParams{Plugin: "v3", Path: path}))
		return err
	}
	fcall := func(fn func(context.Context, *rpc.Call) (any, error), admin bool, path string) error {
		_, err := fn(context.Background(), call(t, admin, FileParams{Plugin: "v3", Path: path, Data: "x"}))
		return err
	}
	adminFile := filepath.Join(e.admin, "a.txt")
	// Admin folder: root bridge yes; user bridge asks for admin.
	if err := read(true, adminFile); err != nil {
		t.Errorf("admin folder on the root bridge: %v", err)
	}
	if os.Geteuid() != 0 {
		if err := read(false, adminFile); !rpc.IsCode(err, rpc.NeedsAdmin) {
			t.Errorf("admin folder on the user bridge: %v", err)
		}
	}
	// Plain folders still never on the root bridge.
	os.WriteFile(filepath.Join(e.ro, "r.txt"), []byte("r"), 0o644)
	if err := read(true, filepath.Join(e.ro, "r.txt")); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("plain folder on the root bridge: %v", err)
	}
	// adminUnlessGroup: the member uses it as themselves.
	if err := fcall(writePluginFile, false, filepath.Join(e.gadmin, "g.txt")); err != nil {
		t.Errorf("adminUnlessGroup member write: %v", err)
	}

	// create: true makes the folder (and parents) on the first write.
	if err := fcall(writePluginFile, false, filepath.Join(e.auto, "settings.json")); err != nil {
		t.Fatalf("write into a create folder: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(e.auto, "settings.json")); string(b) != "x" {
		t.Fatalf("content %q", b)
	}
	// Without create, a missing folder stays missing.
	os.RemoveAll(e.rw)
	if err := fcall(writePluginFile, false, filepath.Join(e.rw, "a")); err == nil {
		t.Error("wrote into a missing folder without create")
	}
	os.MkdirAll(e.rw, 0o755)

	// mkdir / remove, only in write folders.
	nested := filepath.Join(e.rw, "a", "b", "c")
	if err := fcall(mkdirPlugin, false, nested); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if fi, err := os.Stat(nested); err != nil || !fi.IsDir() {
		t.Fatalf("mkdir made nothing: %v", err)
	}
	if err := fcall(mkdirPlugin, false, nested); err != nil {
		t.Fatalf("mkdir of an existing folder: %v", err)
	}
	if err := fcall(mkdirPlugin, false, filepath.Join(e.ro, "new")); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("mkdir in a read folder: %v", err)
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(e.rw, "escape"))
	if err := fcall(mkdirPlugin, false, filepath.Join(e.rw, "escape", "x")); err == nil {
		t.Error("mkdir through a symlink out of the folder")
	}
	if _, err := os.Stat(filepath.Join(outside, "x")); err == nil {
		t.Error("folder created outside")
	}
	if err := fcall(removePlugin, false, filepath.Join(e.rw, "a")); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("remove of a non-empty folder: %v", err)
	}
	if err := fcall(removePlugin, false, nested); err != nil {
		t.Errorf("remove empty folder: %v", err)
	}
	os.WriteFile(filepath.Join(e.rw, "f.txt"), []byte("f"), 0o644)
	if err := fcall(removePlugin, false, filepath.Join(e.rw, "f.txt")); err != nil {
		t.Errorf("remove file: %v", err)
	}
	if err := fcall(removePlugin, false, e.rw); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("remove of the declared folder: %v", err)
	}
	if err := fcall(removePlugin, false, filepath.Join(e.ro, "r.txt")); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("remove in a read folder: %v", err)
	}
	// Removing the symlink removes the link, not its target.
	if err := fcall(removePlugin, false, filepath.Join(e.rw, "escape")); err != nil {
		t.Errorf("remove symlink: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("symlink target removed")
	}
	if err := fcall(removePlugin, false, filepath.Join(e.rw, "..", "ro", "r.txt")); err == nil {
		t.Error("removed through ..")
	}
	if err := fcall(removePlugin, true, filepath.Join(e.rw, "x")); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("remove in a plain folder from the root bridge: %v", err)
	}
}
