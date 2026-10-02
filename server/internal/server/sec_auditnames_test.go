package server

import (
	"bytes"
	"encoding/json"
	"log"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditKeepsOnlyNameLikeValues(t *testing.T) {
	e := newXferEnv(t)
	rec := e.srv.transferRecordFor("ann", "", &transferRequest{Kind: "download", Plugin: `=HYPERLINK("http://x")`, Name: "@SUM(1)", Method: "GET", Path: "/x"})
	if rec.e.Plugin != "?" || rec.e.Via != "?" {
		t.Fatalf("%+v", rec.e)
	}
}

func TestKeyLoginDoesNotLogAnInvalidName(t *testing.T) {
	ts, srv := newKeyTestServer(t, filepath.Join(t.TempDir(), "none"))
	var buf bytes.Buffer
	srv.log = log.New(&buf, "", 0)
	bad := `=cmd|' /C calc'!A1`
	body, _ := json.Marshal(map[string]any{"user": bad, "publicKey": "x", "signature": "x", "nonce": "x"})
	if code, _ := do(t, "POST", ts.URL+"/api/auth/login-key", string(body), true); code == 200 {
		t.Fatal("signed in")
	}
	if strings.Contains(buf.String(), "calc") || !strings.Contains(buf.String(), "invalid user name") {
		t.Fatalf("log: %s", buf.String())
	}
}
