package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ervisio/ervisio/server/internal/audit"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

func rpcOK(t *testing.T, e *xferEnv, method string, params any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"method": method, "params": params})
	code, body := doc(t, e.cl, "POST", e.ts.URL+"/api/rpc", string(b), true)
	var out map[string]any
	json.Unmarshal([]byte(body), &out)
	if code != 200 {
		t.Fatalf("%s: %d %s", method, code, body)
	}
	r, _ := out["result"].(map[string]any)
	return r
}

// Mutating plugin calls are recorded with their result; reads and secrets are not.
func TestAuditPluginCalls(t *testing.T) {
	e := newXferEnv(t)
	// A command: recorded with its exit code.
	rpcOK(t, e, "plugins.exec", map[string]any{"plugin": "xfer", "command": "seq", "args": []string{"3"}})
	// POST with a secret header and a secret query value: the header is never stored, the query value is replaced.
	rpcOK(t, e, "plugins.http", map[string]any{"plugin": "xfer", "name": "svc", "method": "POST", "path": "/act", "query": "name=a&token=hunter2&authconfig=zzz",
		"headers": map[string]string{"X-Registry-Auth": "SECRET-HEADER-VALUE"}, "body": "SECRET-BODY"})
	// A GET is a read: not recorded.
	rpcOK(t, e, "plugins.http", map[string]any{"plugin": "xfer", "name": "svc", "method": "GET", "path": "/dl/5"})
	// A refused call (undeclared path) is recorded as such.
	b, _ := json.Marshal(map[string]any{"method": "plugins.http", "params": map[string]any{"plugin": "xfer", "name": "svc", "method": "POST", "path": "/secret"}})
	if code, _ := doc(t, e.cl, "POST", e.ts.URL+"/api/rpc", string(b), true); code != 400 {
		t.Fatalf("undeclared path: %d", code)
	}
	l := e.entries(t, audit.Query{Plugin: "xfer"})
	if len(l) != 3 {
		t.Fatalf("%d entries, want 3 (exec, POST, refused): %+v", len(l), l)
	}
	byAction := map[string][]audit.Entry{}
	for _, x := range l {
		byAction[x.Action] = append(byAction[x.Action], x)
	}
	c := byAction["command"][0]
	if c.Target != "seq 3" || c.Result != audit.OK || c.Code == nil || *c.Code != 0 || c.IP != "127.0.0.1" || c.User == "" || c.Source != audit.SourcePlugin {
		t.Fatalf("command entry %+v", c)
	}
	var post, refused audit.Entry
	for _, x := range byAction["http"] {
		if strings.Contains(x.Target, "/act") {
			post = x
		} else {
			refused = x
		}
	}
	if post.Target != "POST /act?name=a&token=***&authconfig=***" || post.Result != audit.Failed || post.Code == nil || *post.Code != 409 || post.Via != "svc" {
		t.Fatalf("http entry %+v", post)
	}
	if refused.Target != "POST /secret" || refused.Result != audit.Error || !strings.Contains(refused.Detail, "invalid") {
		t.Fatalf("refused entry %+v", refused)
	}
	raw, _ := os.ReadFile(filepath.Join(e.srv.audit.Dir(), "audit-"+time.Now().UTC().Format("2006-01-02")+".jsonl"))
	for _, secret := range []string{"SECRET-HEADER-VALUE", "SECRET-BODY", "hunter2", "zzz"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("%q is in the activity log:\n%s", secret, raw)
		}
	}

	// plugins.audit.list: the plugin's entries, scoped to it; limit and cursor work.
	r := rpcOK(t, e, "plugins.audit.list", map[string]any{"plugin": "xfer", "limit": 2})
	ents, _ := r["entries"].([]any)
	if len(ents) != 2 || r["next"] == "" {
		t.Fatalf("first page: %v", r)
	}
	r2 := rpcOK(t, e, "plugins.audit.list", map[string]any{"plugin": "xfer", "limit": 2, "cursor": r["next"]})
	if ents2, _ := r2["entries"].([]any); len(ents2) != 1 || r2["next"] != "" {
		t.Fatalf("second page: %v", r2)
	}
	// A plugin cannot read the core entries or another plugin's.
	e.srv.audit.Add(audit.Entry{User: "x", Source: audit.SourcePlugin, Plugin: "other", Action: "command", Target: "rm"})
	e.srv.audit.Add(audit.Entry{User: "x", Source: audit.SourceCore, Action: "login"})
	r = rpcOK(t, e, "plugins.audit.list", map[string]any{"plugin": "xfer", "limit": 100})
	for _, x := range r["entries"].([]any) {
		if x.(map[string]any)["plugin"] != "xfer" {
			t.Fatalf("foreign entry %v", x)
		}
	}
	// A non-administrator sees only their own entries, whatever they ask for.
	r = rpcOK(t, e, "audit.list", map[string]any{"limit": 100, "user": "x"})
	for _, x := range r["entries"].([]any) {
		if x.(map[string]any)["user"] == "x" {
			t.Fatalf("another user's entry: %v", x)
		}
	}
	// Bad parameters.
	b, _ = json.Marshal(map[string]any{"method": "plugins.audit.list", "params": map[string]any{}})
	if code, _ := doc(t, e.cl, "POST", e.ts.URL+"/api/rpc", string(b), true); code != 400 {
		t.Fatalf("plugins.audit.list without a plugin: %d", code)
	}
	b, _ = json.Marshal(map[string]any{"method": "audit.list", "params": map[string]any{"cursor": "bogus"}})
	if code, _ := doc(t, e.cl, "POST", e.ts.URL+"/api/rpc", string(b), true); code != 400 {
		t.Fatalf("bad cursor: %d", code)
	}

	// The export has the same scope, as CSV and JSON.
	code, body := doc(t, e.cl, "GET", e.ts.URL+"/api/audit/export?format=csv&plugin=xfer", "", false)
	if code != 200 || !strings.HasPrefix(body, "time,user,ip,source,plugin,action,via,target,result,code,bytes,admin,detail,env,origin\n") || strings.Count(body, "\n") != 4 {
		t.Fatalf("csv: %d %q", code, body)
	}
	code, body = doc(t, e.cl, "GET", e.ts.URL+"/api/audit/export?format=json&plugin=xfer", "", false)
	var arr []audit.Entry
	if code != 200 || json.Unmarshal([]byte(body), &arr) != nil || len(arr) != 3 {
		t.Fatalf("json: %d %s", code, body)
	}
	if code, _ = doc(t, e.cl, "GET", e.ts.URL+"/api/audit/export?format=xml", "", false); code != 400 {
		t.Fatalf("format: %d", code)
	}
	if code, _ = doc(t, http.DefaultClient, "GET", e.ts.URL+"/api/audit/export?format=csv", "", false); code != 401 {
		t.Fatalf("anonymous export: %d", code)
	}
	if code, _ = doc(t, http.DefaultClient, "POST", e.ts.URL+"/api/rpc", `{"method":"audit.list"}`, true); code != 401 {
		t.Fatalf("anonymous list: %d", code)
	}
}

// Streams (execStream) are recorded when they end, with the exit code.
func TestAuditStream(t *testing.T) {
	e := newXferEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.ts.URL, "http")+"/api/ws", &websocket.DialOptions{HTTPHeader: cookieHeader(e.cl, e.ts.URL)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.Write(ctx, websocket.MessageText, []byte(`{"ch":1,"op":"open","method":"plugins.execStream","params":{"plugin":"xfer","command":"seq","args":["4"]}}`))
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var f wsFrame
		json.Unmarshal(data, &f)
		if f.Op == "end" {
			break
		}
		if f.Op == "error" {
			t.Fatalf("%s", data)
		}
	}
	l := e.entries(t, audit.Query{Action: "command"})
	if len(l) != 1 || l[0].Target != "seq 4" || l[0].Result != audit.OK || l[0].Code == nil || *l[0].Code != 0 {
		t.Fatalf("%+v", l)
	}
}

func TestAuditDisabledAndCore(t *testing.T) {
	e := newXferEnv(t)
	e.srv.auditCore("bob", "10.0.0.5", "login", "password", audit.OK, "")
	e.srv.auditCore("bob", "10.0.0.5", "login.failed", "password", audit.Denied, "wrong password")
	l := e.entries(t, audit.Query{Source: audit.SourceCore})
	if len(l) != 2 || l[0].Action != "login.failed" || l[1].IP != "10.0.0.5" {
		t.Fatalf("%+v", l)
	}
	// With audit.enabled = false nothing more is written.
	e.srv.cfg.cfg.Audit.Enabled = false
	e.srv.cfg.checked = time.Now().Add(time.Hour)
	e.srv.auditCore("bob", "10.0.0.5", "login", "", audit.OK, "")
	rpcOK(t, e, "plugins.exec", map[string]any{"plugin": "xfer", "command": "seq", "args": []string{"2"}})
	if l := e.entries(t, audit.Query{}); len(l) != 2 {
		t.Fatalf("entries written while disabled: %+v", l)
	}
}

// A call proxied from a paired server carries via/env params: both end up in the entry.
func TestAuditEnvAndOrigin(t *testing.T) {
	e := newXferEnv(t)
	rec := e.srv.auditBeginFor("ann", "", "plugins.exec", json.RawMessage(`{"plugin":"xfer","command":"seq","args":["2"],"env":"env-aabbccdd","via":"box by ann"}`), false)
	rec.callDone(json.RawMessage(`{"exitCode":0}`), nil)
	l := e.entries(t, audit.Query{Action: "command"})
	if len(l) != 1 || l[0].Env != "env-aabbccdd" || l[0].Origin != "box by ann" || l[0].User != "ann" {
		t.Fatalf("%+v", l)
	}
	// The relay of a paired server records transfers too, with the origin.
	m := rpc.Message{ID: 1, Method: "plugins.httpDownload", Params: json.RawMessage(`{"plugin":"xfer","name":"svc","method":"GET","path":"/dl/5"}`)}
	rec = e.srv.pairAudit("ann", "", m, "box by bob")
	rec.observe(rpc.Event{Data: json.RawMessage(`{"status":200}`)})
	rec.streamDone(nil)
	l = e.entries(t, audit.Query{Action: "download"})
	if len(l) != 1 || l[0].Origin != "box by bob" || l[0].User != "ann" || l[0].Result != audit.OK {
		t.Fatalf("%+v", l)
	}
	// A transfer for an environment that does not exist is refused.
	st, out := e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/5", "env": "env-00000000"})
	if st != 404 || !strings.Contains(fmt.Sprint(out), "no environment") {
		t.Fatalf("env: %d %v", st, out)
	}
	req := &transferRequest{Kind: "download", Name: "svc", Method: "GET", Path: "/dl/5", Env: "env-aabbccdd"}
	if _, p, _ := req.bridge(); p["env"] != "env-aabbccdd" || p["envSocket"] != nil {
		t.Fatalf("bridge params %v", p)
	}
}
