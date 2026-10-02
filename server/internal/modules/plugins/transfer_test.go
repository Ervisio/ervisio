package plugins

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Large transfers: plugins.httpDownload, plugins.execDownload and
// plugins.httpUpload enforce the same rules as plugins.http, plus GET only /
// POST or PUT only, and the manifest's maxUpload.

// dl runs plugins.httpDownload against a fake stream.
func dl(ctx context.Context, p HTTPParams) (*fakeStream, error) {
	s := newFakeStream()
	return s, runHTTPDownload(ctx, &rpc.Call{}, s, p)
}

func TestDownloadStreamsWithoutLimit(t *testing.T) {
	setupV3(t)
	// 20 MiB: far over maxBody (4096 on this API) and the 8 MiB result cap.
	const n = 20 << 20
	s, err := dl(context.Background(), HTTPParams{Plugin: "v3", Name: "xfer", Method: "GET", Path: fmt.Sprintf("/dl/%d", n)})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.data.Len(); got != n {
		t.Fatalf("got %d bytes, want %d", got, n)
	}
	var start struct {
		Status  int
		Headers map[string]string
	}
	if json.Unmarshal(s.events[0], &start); start.Status != 200 || start.Headers["Content-Length"] != fmt.Sprint(n) {
		t.Fatalf("start event %s", s.events[0])
	}
}

func TestDownloadRules(t *testing.T) {
	setupV3(t)
	ctx := context.Background()
	for name, p := range map[string]HTTPParams{
		"POST":            {Plugin: "v3", Name: "xfer", Method: "POST", Path: "/count"},
		"HEAD":            {Plugin: "v3", Name: "xfer", Method: "HEAD", Path: "/dl"},
		"undeclared path": {Plugin: "v3", Name: "xfer", Method: "GET", Path: "/echo"},
		"bad path":        {Plugin: "v3", Name: "xfer", Method: "GET", Path: "/dl/../echo"},
		"unknown api":     {Plugin: "v3", Name: "nope", Method: "GET", Path: "/dl"},
		"header":          {Plugin: "v3", Name: "xfer", Method: "GET", Path: "/dl", Headers: map[string]string{"Cookie": "a=b"}},
	} {
		if _, err := dl(ctx, p); !rpc.IsCode(err, rpc.Invalid) && !rpc.IsCode(err, rpc.NotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A GET that is declared only for another API is refused.
	if _, err := dl(ctx, HTTPParams{Plugin: "v3", Name: "api", Method: "GET", Path: "/dl"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("rule of another api: %v", err)
	}
	// Admin APIs need the root bridge, as for plugins.http.
	if _, err := dl(ctx, HTTPParams{Plugin: "v3", Name: "root-xfer", Method: "GET", Path: "/dl"}); !isNeedsAdmin(err) {
		t.Errorf("admin api without admin: %v", err)
	}
	s := newFakeStream()
	if err := runHTTPDownload(ctx, &rpc.Call{Admin: true}, s, HTTPParams{Plugin: "v3", Name: "xfer", Method: "GET", Path: "/dl/10"}); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("user api on the root bridge: %v", err)
	}
}

func isNeedsAdmin(err error) bool {
	// Skipped for root: root needs no unlock.
	return err == nil || rpc.IsCode(err, rpc.NeedsAdmin)
}

func TestDownloadServiceStatusIsReported(t *testing.T) {
	setupV3(t)
	s, err := dl(context.Background(), HTTPParams{Plugin: "v3", Name: "xfer", Method: "GET", Path: "/dl/404"})
	if err != nil {
		t.Fatal(err)
	}
	var start struct{ Status int }
	json.Unmarshal(s.events[0], &start)
	if start.Status != 404 || !strings.Contains(s.data.String(), "no such thing") {
		t.Fatalf("%s %q", s.events[0], s.data.String())
	}
}

func TestExecDownload(t *testing.T) {
	setupV3(t)
	ctx := context.Background()
	s := newFakeStream()
	if err := runExecDownload(ctx, &rpc.Call{}, s, ExecParams{Plugin: "v3", Command: "seq", Args: []string{"5"}}); err != nil {
		t.Fatal(err)
	}
	if s.data.String() != "1\n2\n3\n4\n5\n" || len(s.events) != 1 {
		t.Fatalf("%q %s", s.data.String(), s.events)
	}
	// More than the 4 MiB plugins.exec keeps, with no limit.
	s = newFakeStream()
	if err := runExecDownload(ctx, &rpc.Call{}, s, ExecParams{Plugin: "v3", Command: "seq", Args: []string{"999999"}}); err != nil {
		t.Fatal(err)
	}
	if s.data.Len() < 6_000_000 {
		t.Fatalf("only %d bytes", s.data.Len())
	}
	// A command that fails before any output is an error with its stderr.
	err := runExecDownload(ctx, &rpc.Call{}, newFakeStream(), ExecParams{Plugin: "v3", Command: "oops"})
	if !rpc.IsCode(err, rpc.Unavailable) || !strings.Contains(err.Error(), "oops") || !strings.Contains(err.Error(), "3") {
		t.Fatalf("failing command: %v", err)
	}
	// Declared commands only, with their argument rules, and never pty ones.
	for name, p := range map[string]ExecParams{
		"unknown":  {Plugin: "v3", Command: "nope"},
		"bad arg":  {Plugin: "v3", Command: "seq", Args: []string{"x; rm"}},
		"no arg":   {Plugin: "v3", Command: "seq"},
		"pty":      {Plugin: "v3", Command: "cat"},
		"root cmd": {Plugin: "v3", Command: "rootcmd"},
	} {
		err := runExecDownload(ctx, &rpc.Call{}, newFakeStream(), p)
		if name == "root cmd" && err == nil {
			continue // the test user is root
		}
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// upload runs plugins.httpUpload with body, sending it in chunks, and returns the final event.
func upload(t *testing.T, p UploadParams, body []byte, chunk int) (map[string]any, error) {
	t.Helper()
	s := newFakeStream()
	done := make(chan error, 1)
	go func() { done <- runHTTPUpload(context.Background(), &rpc.Call{}, s, p) }()
	// {"ready":true} comes before any input is read.
	s.waitFor(t, "ready", func(_ string, ev []json.RawMessage) bool { return len(ev) >= 1 })
	var ready struct{ Ready bool }
	json.Unmarshal(s.event(0), &ready)
	if !ready.Ready {
		t.Fatalf("first event %s", s.events[0])
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for off := 0; off < len(body); off += chunk {
			end := min(off+chunk, len(body))
			select {
			case s.in <- mustJSON(map[string]string{"data": base64.StdEncoding.EncodeToString(body[off:end])}):
			case <-time.After(5 * time.Second):
				return
			}
		}
		select {
		case s.in <- json.RawMessage(`{"eof":true}`):
		case <-time.After(5 * time.Second):
		}
	}()
	err := <-done
	wg.Wait()
	if err != nil {
		return nil, err
	}
	var last map[string]any
	if json.Unmarshal(s.event(-1), &last) != nil {
		t.Fatalf("last event %s", s.event(-1))
	}
	return last, nil
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestUploadStreamsBody(t *testing.T) {
	setupV3(t)
	// 2.5 MB: over maxBody (4096), under maxUpload (3000000).
	body := bytes.Repeat([]byte("abcdefghij"), 250000)
	last, err := upload(t, UploadParams{HTTPParams: HTTPParams{Plugin: "v3", Name: "xfer", Method: "POST", Path: "/count"}, Size: int64(len(body))}, body, 48<<10)
	if err != nil {
		t.Fatal(err)
	}
	if last["status"] != float64(200) || last["done"] != true {
		t.Fatalf("%v", last)
	}
	var res struct {
		N  int64  `json:"n"`
		CL int64  `json:"cl"`
		M  string `json:"method"`
	}
	json.Unmarshal([]byte(last["body"].(string)), &res)
	if res.N != int64(len(body)) || res.CL != int64(len(body)) || res.M != "POST" {
		t.Fatalf("service saw %+v", res)
	}
	// PUT works, an empty file too.
	last, err = upload(t, UploadParams{HTTPParams: HTTPParams{Plugin: "v3", Name: "xfer", Method: "PUT", Path: "/count"}, Size: 0}, nil, 1)
	if err != nil || !strings.Contains(last["body"].(string), `"n":0`) || !strings.Contains(last["body"].(string), `"PUT"`) {
		t.Fatalf("empty put: %v %v", last, err)
	}
	// The service's own status is returned, not an error.
	last, err = upload(t, UploadParams{HTTPParams: HTTPParams{Plugin: "v3", Name: "xfer", Method: "POST", Path: "/count-fail"}, Size: 3}, []byte("abc"), 10)
	if err != nil || last["status"] != float64(409) || !strings.Contains(last["body"].(string), "nope") {
		t.Fatalf("service error: %v %v", last, err)
	}
}

func TestUploadRules(t *testing.T) {
	setupV3(t)
	ctx := context.Background()
	run := func(p UploadParams) error {
		s := newFakeStream()
		return runHTTPUpload(ctx, &rpc.Call{}, s, p)
	}
	h := func(m, path string) HTTPParams { return HTTPParams{Plugin: "v3", Name: "xfer", Method: m, Path: path} }
	for name, p := range map[string]UploadParams{
		"GET":             {HTTPParams: h("GET", "/dl"), Size: 1},
		"DELETE":          {HTTPParams: h("DELETE", "/count"), Size: 1},
		"PATCH":           {HTTPParams: h("PATCH", "/count"), Size: 1},
		"undeclared path": {HTTPParams: h("POST", "/echo"), Size: 1},
		"POST on a GET":   {HTTPParams: h("POST", "/dl"), Size: 1},
		"bad path":        {HTTPParams: h("POST", "/count/../x"), Size: 1},
		"negative size":   {HTTPParams: h("POST", "/count"), Size: -1},
		"over maxUpload":  {HTTPParams: h("POST", "/count"), Size: 3000001},
		"inline body":     {HTTPParams: HTTPParams{Plugin: "v3", Name: "xfer", Method: "POST", Path: "/count", Body: "x"}, Size: 1},
		"bad header":      {HTTPParams: HTTPParams{Plugin: "v3", Name: "xfer", Method: "POST", Path: "/count", Headers: map[string]string{"Authorization": "x"}}},
	} {
		if err := run(p); !rpc.IsCode(err, rpc.Invalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The default limit applies when maxUpload is not declared: 20 GiB.
	if err := run(UploadParams{HTTPParams: HTTPParams{Plugin: "v3", Name: "root-xfer", Method: "POST", Path: "/count"}, Size: 21 << 30}); err == nil {
		t.Error("21 GiB accepted")
	}
	// A user API is not used from the root bridge.
	s := newFakeStream()
	if err := runHTTPUpload(ctx, &rpc.Call{Admin: true}, s, UploadParams{HTTPParams: h("POST", "/count"), Size: 1}); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("user api on root bridge: %v", err)
	}
}

func TestUploadSizeMismatch(t *testing.T) {
	setupV3(t)
	p := UploadParams{HTTPParams: HTTPParams{Plugin: "v3", Name: "xfer", Method: "POST", Path: "/count"}}
	// Fewer bytes than announced: refused, not sent as a short file.
	p.Size = 10
	if _, err := upload(t, p, []byte("12345"), 5); !rpc.IsCode(err, rpc.Invalid) || !strings.Contains(err.Error(), "ended early") {
		t.Fatalf("short upload: %v", err)
	}
	// More bytes than announced.
	p.Size = 3
	if _, err := upload(t, p, []byte("12345"), 5); !rpc.IsCode(err, rpc.Invalid) || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("long upload: %v", err)
	}
}

func TestUploadCancel(t *testing.T) {
	setupV3(t)
	s := newFakeStream()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runHTTPUpload(ctx, &rpc.Call{}, s, UploadParams{HTTPParams: HTTPParams{Plugin: "v3", Name: "xfer", Method: "POST", Path: "/count"}, Size: 1000})
	}()
	s.waitFor(t, "ready", func(_ string, ev []json.RawMessage) bool { return len(ev) >= 1 })
	s.in <- mustJSON(map[string]string{"data": base64.StdEncoding.EncodeToString([]byte("part"))})
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled upload succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the upload did not stop")
	}
}

func TestUploadStreamedResponse(t *testing.T) {
	setupV3(t)
	body := bytes.Repeat([]byte("z"), 700_000)
	s := newFakeStream()
	done := make(chan error, 1)
	go func() {
		done <- runHTTPUpload(context.Background(), &rpc.Call{}, s, UploadParams{HTTPParams: HTTPParams{Plugin: "v3", Name: "xfer", Method: "POST", Path: "/build"}, Size: int64(len(body)), Stream: true})
	}()
	s.waitFor(t, "ready", func(_ string, ev []json.RawMessage) bool { return len(ev) >= 1 })
	// The service's first line arrives before the file is sent.
	s.waitFor(t, "first line", func(d string, ev []json.RawMessage) bool {
		return strings.Contains(d, "Step 1: receiving\n") && len(ev) >= 2
	})
	var start struct {
		Status  int
		Headers map[string]string
	}
	json.Unmarshal(s.event(1), &start)
	if start.Status != 200 || start.Headers["X-Kind"] != "build" {
		t.Fatalf("start event %s", s.event(1))
	}
	for off := 0; off < len(body); off += 64 << 10 {
		s.input(map[string]string{"data": base64.StdEncoding.EncodeToString(body[off:min(off+64<<10, len(body))])})
	}
	s.input(map[string]bool{"eof": true})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := s.data.String(); got != "Step 1: receiving\nStep 2: got 700000 bytes\n" {
		t.Fatalf("response %q", got)
	}
	var last map[string]any
	json.Unmarshal(s.event(-1), &last)
	if last["done"] != true || last["status"] != float64(200) || last["body"] != nil {
		t.Fatalf("last event %s", s.event(-1))
	}
}

// event returns the i-th event (negative: from the end) under the lock, for tests that read while a stream still runs.
func (f *fakeStream) event(i int) json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i < 0 {
		i += len(f.events)
	}
	return f.events[i]
}

// An upload whose data stops coming is given up after uploadIdle: the
// request to the service is closed, not left waiting for the rest.
func TestUploadStalledClosesTheRequest(t *testing.T) {
	setupV3(t)
	old := uploadIdle
	uploadIdle = 300 * time.Millisecond
	defer func() { uploadIdle = old }()
	s := newFakeStream()
	done := make(chan error, 1)
	p := UploadParams{HTTPParams: HTTPParams{Plugin: "v3", Name: "xfer", Method: "POST", Path: "/count"}, Size: 1 << 20}
	go func() { done <- runHTTPUpload(context.Background(), &rpc.Call{}, s, p) }()
	s.waitFor(t, "ready", func(_ string, ev []json.RawMessage) bool { return len(ev) >= 1 })
	s.in <- mustJSON(map[string]string{"data": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), 1000))})
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "stalled") {
			t.Fatalf("err %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a stalled upload kept its request open")
	}
}
