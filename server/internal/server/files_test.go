package server

import (
	"bytes"
	"crypto/rand"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileTransfer(t *testing.T) {
	// The real bridge: files.readStream / files.writeStream live in
	// internal/modules/files.
	bridgeBin := buildBridge(t)
	srv, err := New(Options{ConfigPath: filepath.Join(t.TempDir(), "c.conf"), Dev: true, NoAuth: true,
		WebDir: t.TempDir(), Bridge: bridgeBin, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer func() {
		ts.Close()
		for _, s := range srv.sessions.all() {
			srv.sessions.remove(s)
		}
	}()

	dir := t.TempDir()
	payload := make([]byte, 1<<20+12345) // several chunks, more than the window
	rand.Read(payload)
	dst := filepath.Join(dir, "up.bin")

	upload := func(overwrite string) (int, string) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/files/upload?path="+url.QueryEscape(dst)+"&overwrite="+overwrite, bytes.NewReader(payload))
		req.Header.Set("X-Requested-With", "linuxadmin")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := upload("0"); code != 200 || !strings.Contains(body, `"done":true`) {
		t.Fatal(code, body)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, payload) {
		t.Fatal("uploaded content differs")
	}
	if code, body := upload("0"); code != 409 {
		t.Fatal("expected conflict", code, body)
	}

	resp, err := http.Get(ts.URL + "/api/files/download?path=" + url.QueryEscape(dst))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(body, payload) {
		t.Fatalf("download: %d, %d bytes", resp.StatusCode, len(body))
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "attachment; filename*=UTF-8''up.bin" {
		t.Fatalf("disposition %q", cd)
	}
	resp, _ = http.Get(ts.URL + "/api/files/download?path=" + url.QueryEscape(filepath.Join(dir, "missing")))
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("missing file: %d", resp.StatusCode)
	}
}
