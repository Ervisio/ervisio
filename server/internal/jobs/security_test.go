package jobs

import (
	"context"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/modules/plugins"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Regression tests for the 0.5 security review (H2, M1, L5).

// H2: a wheel member whose session is not unlocked (a plugin frame passing
// confirmAdmin: true) must not get an instance that runs a step as root,
// neither directly nor through a webhook.
func TestRootJobNeedsAnUnlockedApproval(t *testing.T) {
	f := newFixture(t)
	c := f.caller("alice") // wheel, Admin false: no password typed
	v, err := f.m.Create(c, CreateReq{Plugin: "jt", Job: "rooty", ConfirmAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Approval != nil || !v.AwaitingApproval {
		t.Fatalf("confirmAdmin from a plugin approved the instance: %+v", v)
	}
	wh, err := f.m.CreateWebhook(c, "jt", v.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	res := f.m.HandleHook("jt", wh.Token, "203.0.113.9", nil, url.Values{})
	f.m.Wait()
	if res.Status == 202 || len(f.exec.calls) != 0 {
		t.Fatalf("an unapproved instance ran through a webhook: %+v %+v", res, f.exec.calls)
	}
	// An update with confirmAdmin does not approve it either.
	if _, err := f.m.Update(c, UpdateReq{Plugin: "jt", ID: v.ID, Params: &map[string]string{}, ConfirmAdmin: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.m.Get(c, "jt", v.ID); got.Approval != nil {
		t.Fatalf("update approved it: %+v", got)
	}
	if _, err := f.m.Approve(c, v.ID); err == nil {
		t.Fatal("Approve without administrator rights now")
	}
}

const paramManifest = `{
  "id": "jt", "name": "J", "version": "1.0.0", "entry": "index.js", "platforms": ["linux", "windows"],
  "capabilities": {
    "commands": [{"name": "rm", "admin": true, "argv": ["rm", "-rf", "/srv/{0}"], "args": [{"pattern": "[a-z]+"}]},
                 {"name": "echo", "argv": ["echo", "{0}"], "args": [{"pattern": "[a-z]+"}]}],
    "jobs": [{"name": "clean", "params": [{"name": "d", "pattern": "[a-z]+"}], "webhook": {"params": ["d"]},
      "steps": [{"id": "a", "command": "rm", "args": ["{param.d}"]}]},
      {"name": "say", "params": [{"name": "d", "pattern": "[a-z]+"}], "webhook": {"params": ["d"]},
      "steps": [{"id": "a", "command": "echo", "args": ["{param.d}"]}]}]
  },
  "contributes": {"pages": [], "widgets": [], "snippets": []}, "visibleTo": {"groups": []}}`

// M1: params sent with a webhook call must not replace the values an
// administrator approved for a job that runs steps as root.
func TestWebhookCannotOverrideApprovedParams(t *testing.T) {
	f := newFixture(t)
	man, err := plugins.ParseManifest([]byte(paramManifest))
	if err != nil {
		t.Fatal(err)
	}
	f.man = man
	v := f.approved("alice", "clean", map[string]string{"d": "cache"}, nil)
	wh, _ := f.m.CreateWebhook(f.caller("alice"), "jt", v.ID, "")
	if r := f.m.HandleHook("jt", wh.Token, "203.0.113.9", []byte(`{"d":"www"}`), url.Values{}); r.Status != 400 {
		t.Fatalf("body param on an admin instance: %+v", r)
	}
	if r := f.m.HandleHook("jt", wh.Token, "203.0.113.9", nil, url.Values{"d": {"www"}}); r.Status != 400 {
		t.Fatalf("query param on an admin instance: %+v", r)
	}
	f.m.Wait()
	if len(f.exec.calls) != 0 {
		t.Fatalf("calls %+v", f.exec.calls)
	}
	// Without params it runs with the approved value.
	if r := f.m.HandleHook("jt", wh.Token, "203.0.113.9", nil, url.Values{}); r.Status != 202 {
		t.Fatalf("plain call: %+v", r)
	}
	f.m.Wait()
	if len(f.exec.calls) != 1 || f.exec.calls[0].args[0] != "cache" || !f.exec.calls[0].admin {
		t.Fatalf("calls %+v", f.exec.calls)
	}
	// A job without admin steps still takes its webhook params.
	s := f.create("alice", "say", map[string]string{"d": "hi"}, nil)
	wh2, _ := f.m.CreateWebhook(f.caller("alice"), "jt", s.ID, "")
	if r := f.m.HandleHook("jt", wh2.Token, "203.0.113.9", []byte(`{"d":"yo"}`), url.Values{}); r.Status != 202 {
		t.Fatalf("user job: %+v", r)
	}
	f.m.Wait()
	if last := f.exec.calls[len(f.exec.calls)-1]; last.args[0] != "yo" || last.admin {
		t.Fatalf("user job call %+v", last)
	}
}

// M1: the approval covers the param values; new values wait for a new one.
func TestApprovalCoversParamValues(t *testing.T) {
	f := newFixture(t)
	man, err := plugins.ParseManifest([]byte(paramManifest))
	if err != nil {
		t.Fatal(err)
	}
	f.man = man
	v := f.approved("alice", "clean", map[string]string{"d": "cache"}, nil)
	got, err := f.m.Update(f.caller("alice"), UpdateReq{Plugin: "jt", ID: v.ID, Params: &map[string]string{"d": "www"}})
	if err != nil {
		t.Fatal(err)
	}
	if !got.AwaitingApproval || got.Approval != nil {
		t.Fatalf("new values kept the approval: %+v", got)
	}
	if _, err := f.m.RunNow(f.caller("alice"), "jt", v.ID); err == nil {
		t.Fatal("ran with unapproved values")
	}
	f.m.Wait()
	if len(f.exec.calls) != 0 {
		t.Fatalf("calls %+v", f.exec.calls)
	}
	// An instance stored with a signature that did not cover the values
	// (or a tampered one) is not valid for other values either.
	f.m.mu.Lock()
	f.m.findLocked(v.ID).Approval = &Approval{By: "alice", Sig: approvalSig(man, man.Job("clean"), map[string]string{"d": "cache"})}
	f.m.mu.Unlock()
	if g, _ := f.m.Get(f.caller("alice"), "jt", v.ID); g.Approval == nil || g.Approval.Valid {
		t.Fatalf("signature for other values counted: %+v", g)
	}
}

// L5: the plugin turned off between the check and the steps must not crash
// the daemon (it used to look the manifest up a second time).
func TestManifestGoneDuringRunDoesNotPanic(t *testing.T) {
	f := newFixture(t)
	v := f.create("bob", "simple", nil, nil)
	var n int32
	orig := f.m.env.Manifest
	f.m.env.Manifest = func(id string) (*plugins.Manifest, error) {
		if atomic.AddInt32(&n, 1) >= 2 {
			return nil, rpc.Errorf(rpc.Forbidden, "turned off")
		}
		return orig(id)
	}
	in, _ := f.m.find(v.ID)
	status, msg, _ := f.m.runSteps(t.Context(), in, &Run{}, Trigger{Kind: "manual"})
	if status != "ok" {
		t.Fatalf("%s %s", status, msg)
	}
}

type panicExec struct{ fakeExec }

func (p *panicExec) Exec(context.Context, bool, plugins.ExecParams) (*plugins.ExecResult, error) {
	panic("boom")
}

// L5: a panic during a run ends the run as failed instead of the daemon.
func TestPanicInARunFailsTheRun(t *testing.T) {
	f := newFixture(t)
	f.m.env.NewExecutor = func(*account.Account) Executor { return &panicExec{} }
	v := f.create("bob", "simple", nil, nil)
	if _, err := f.m.RunNow(f.caller("bob"), "jt", v.ID); err != nil {
		t.Fatal(err)
	}
	f.m.Wait()
	r := lastRun(f, v.ID)
	if r.Status != "failed" || !strings.Contains(r.Error, "internal error") {
		t.Fatalf("%+v", r)
	}
}
