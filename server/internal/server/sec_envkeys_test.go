package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ervisio/ervisio/server/internal/audit"
)

func TestEnvKeysAreStrippedInAnyCase(t *testing.T) {
	e := newXferEnv(t)
	sess := e.srv.sessions.all()[0]
	for _, params := range []string{
		`{"plugin":"xfer","name":"svc","method":"GET","path":"/dl/1","EnvSocket":"/run/user/1000/evil.sock"}`,
		`{"plugin":"xfer","name":"svc","method":"GET","path":"/dl/1","ENVSOCKET":"/x","Via":"forged"}`,
	} {
		_, _, out, release, rerr := e.srv.routeWithEnv(context.Background(), sess, "plugins.http", json.RawMessage(params), false)
		if rerr != nil {
			t.Fatalf("%s: %v", params, rerr)
		}
		release()
		var p struct {
			EnvSocket string `json:"envSocket"`
			Via       string `json:"via"`
		}
		_ = json.Unmarshal(out, &p)
		if p.EnvSocket != "" || p.Via != "" {
			t.Fatalf("the bridge would see envSocket=%q via=%q from %s", p.EnvSocket, p.Via, out)
		}
	}
	// "ENV" is an environment id like "env": checked, not passed through.
	if _, _, _, _, rerr := e.srv.routeWithEnv(context.Background(), sess, "plugins.http", json.RawMessage(`{"plugin":"xfer","name":"svc","ENV":"env-00000000"}`), false); rerr == nil {
		t.Fatal(`"ENV" was not treated as an environment`)
	}
	// The pairing relay drops every spelling too, then sets its own via.
	out := addVia(json.RawMessage(`{"plugin":"docker","EnvSocket":"/x","Env":"e","VIA":"spoofed"}`), "A by alice")
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	if len(m) != 2 || m["via"] != "A by alice" {
		t.Fatalf("%s", out)
	}
}

func TestBrowserCannotForgeAuditOrigin(t *testing.T) {
	e := newXferEnv(t)
	sess := e.srv.sessions.all()[0]
	rec := e.srv.auditBegin(sess, "", "plugins.exec", json.RawMessage(`{"plugin":"xfer","command":"seq","args":["2"],"Via":"box by root"}`), false)
	rec.callDone(json.RawMessage(`{"exitCode":0}`), nil)
	l := e.entries(t, audit.Query{Action: "command"})
	if len(l) != 1 || l[0].Origin != "" {
		t.Fatalf("%+v", l)
	}
}

// The files methods take the same env option: forged keys are stripped and an
// unknown environment is refused, and a pairing's file writes are audited with
// the origin and without the env.
func TestFilesMethodsTakeEnvLikeTheOthers(t *testing.T) {
	e := newXferEnv(t)
	sess := e.srv.sessions.all()[0]
	_, _, out, release, rerr := e.srv.routeWithEnv(context.Background(), sess, "plugins.writeFile", json.RawMessage(`{"plugin":"xfer","path":"/opt/stacks/a/x","data":"x","EnvSocket":"/x","VIA":"forged"}`), false)
	if rerr != nil {
		t.Fatal(rerr)
	}
	release()
	var p map[string]any
	_ = json.Unmarshal(out, &p)
	if _, ok := p["EnvSocket"]; ok || p["via"] != nil || p["VIA"] != nil {
		t.Fatalf("%s", out)
	}
	if _, _, _, _, rerr := e.srv.routeWithEnv(context.Background(), sess, "plugins.listDir", json.RawMessage(`{"plugin":"xfer","path":"/opt/stacks","Env":"env-00000000"}`), false); rerr == nil {
		t.Fatal("an unknown environment on a files call was accepted")
	}
	for _, m := range []string{"plugins.writeFile", "plugins.mkdir", "plugins.remove"} {
		rec := e.srv.pairAudit("alice", "", rpcMsg(m, `{"plugin":"xfer","path":"/opt/stacks/a","data":"hello","via":"A by bob","env":"env-aabbccdd"}`), "A (pairing p1) by bob")
		if rec == nil {
			t.Fatalf("%s is not audited on the paired side", m)
		}
		if rec.e.Origin != "A (pairing p1) by bob" || rec.e.Env != "" || rec.e.Target != "/opt/stacks/a" {
			t.Fatalf("%s: %+v", m, rec.e)
		}
	}
}
