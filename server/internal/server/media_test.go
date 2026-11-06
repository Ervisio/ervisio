package server

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMediaByteRanges(t *testing.T) {
	for _, tc := range []struct {
		value               string
		size, start, length int64
		partial, bad        bool
	}{
		{"", 100, 0, 100, false, false}, {"bytes=0-0", 100, 0, 1, true, false},
		{"bytes=20-29", 100, 20, 10, true, false}, {"bytes=95-", 100, 95, 5, true, false},
		{"bytes=-12", 100, 88, 12, true, false}, {"bytes=-1000", 100, 0, 100, true, false},
		{"bytes=90-999", 100, 90, 10, true, false}, {"widgets=10-12", 100, 0, 100, false, false},
		{"bytes=100-", 100, 0, 0, false, true}, {"bytes=5-4", 100, 0, 0, false, true},
		{"bytes=0-4,9-10", 100, 0, 0, false, true}, {"bytes=-0", 100, 0, 0, false, true},
		{"bytes=0-", 0, 0, 0, false, true}, {"bytes=99999999999999999999999-", 100, 0, 0, false, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			span, err := parseMediaRange(tc.value, tc.size)
			if (err != nil) != tc.bad {
				t.Fatalf("range=%+v err=%v", span, err)
			}
			if !tc.bad && (span.start != tc.start || span.length != tc.length || span.partial != tc.partial) {
				t.Fatalf("range=%+v", span)
			}
		})
	}
	now := time.Now().Truncate(time.Second)
	r := &http.Request{Method: http.MethodGet, Header: http.Header{"Range": []string{"bytes=1-3"}, "If-Range": []string{now.UTC().Format(http.TimeFormat)}}}
	if mediaRangeHeader(r, now.UnixMilli()) == "" {
		t.Fatal("matching last-modified validator ignored")
	}
	r.Header.Set("If-Range", now.Add(-time.Second).UTC().Format(http.TimeFormat))
	if mediaRangeHeader(r, now.UnixMilli()) != "" {
		t.Fatal("stale validator accepted")
	}
	r.Header.Set("If-Range", `"unavailable-etag"`)
	if mediaRangeHeader(r, now.UnixMilli()) != "" {
		t.Fatal("unsupported validator accepted")
	}
}

func TestMediaTransferRangeHeadPermissionsAndMIME(t *testing.T) {
	ts, srv := newTestServer(t, true)
	client := noAuthClient(t, srv, ts)
	dir := t.TempDir()
	payload := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	file := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(file, payload, 0600); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, span string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, ts.URL+"/api/files/media?path="+url.QueryEscape(path), nil)
		if span != "" {
			req.Header.Set("Range", span)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return res, body
	}
	res, body := request("GET", file, "bytes=10-19")
	if res.StatusCode != http.StatusPartialContent || !bytes.Equal(body, payload[10:20]) || res.Header.Get("Content-Range") != "bytes 10-19/36" || res.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("range: %d %+v %q", res.StatusCode, res.Header, body)
	}
	res, body = request("GET", file, "bytes=-5")
	if res.StatusCode != 206 || !bytes.Equal(body, payload[len(payload)-5:]) {
		t.Fatal("suffix range", res.StatusCode, string(body))
	}
	res, body = request("GET", file, "")
	if res.StatusCode != 200 || !bytes.Equal(body, payload) || res.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatal("full media transfer")
	}
	res, body = request("HEAD", file, "bytes=0-0")
	if res.StatusCode != 200 || len(body) != 0 || res.ContentLength != int64(len(payload)) {
		t.Fatal("HEAD must describe full file without transferring bytes")
	}
	res, _ = request("GET", file, "bytes=100-")
	if res.StatusCode != 416 || res.Header.Get("Content-Range") != "bytes */36" {
		t.Fatal("unsatisfiable range", res.StatusCode)
	}
	html := filepath.Join(dir, "active.html")
	_ = os.WriteFile(html, []byte("<script>alert(1)</script>"), 0600)
	res, _ = request("GET", html, "")
	if res.StatusCode != 400 {
		t.Fatal("active document served inline", res.StatusCode)
	}
	svg := filepath.Join(dir, "drawing.svg")
	_ = os.WriteFile(svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 0600)
	res, _ = request("GET", svg, "")
	if res.StatusCode != 200 || res.Header.Get("Content-Security-Policy") != "sandbox; default-src 'none'" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("SVG must remain sandboxed and type-bound")
	}
	res, _ = request("GET", filepath.Join(dir, "missing.mp4"), "")
	if res.StatusCode != 404 {
		t.Fatal("missing media", res.StatusCode)
	}
	anonymous, err := http.Get(ts.URL + "/api/files/media?path=" + url.QueryEscape(file))
	if err != nil {
		t.Fatal(err)
	}
	_ = anonymous.Body.Close()
	if anonymous.StatusCode != 401 {
		t.Fatal("anonymous media read", anonymous.StatusCode)
	}
}
