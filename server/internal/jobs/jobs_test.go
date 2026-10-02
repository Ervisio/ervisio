package jobs

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/modules/plugins"
	"github.com/ervisio/ervisio/server/internal/notify"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

const testManifest = `{
  "id": "jt", "name": "Jobs test", "version": "1.0.0", "entry": "index.js",
  "capabilities": {
    "notify": true,
    "commands": [
      {"name": "fetch", "argv": ["git", "-C", "{0}", "fetch"], "args": [{"pattern": "/opt/stacks/[a-z0-9_.-]+"}]},
      {"name": "rev", "argv": ["git", "-C", "{0}", "rev-parse", "{1}"], "args": [{"pattern": "/opt/stacks/[a-z0-9_.-]+"}, {"pattern": "HEAD|@\\{u\\}"}]},
      {"name": "pull", "argv": ["git", "-C", "{0}", "pull"], "args": [{"pattern": "/opt/stacks/[a-z0-9_.-]+"}]},
      {"name": "slow", "argv": ["sleep", "100"]},
      {"name": "boom", "argv": ["false"]},
      {"name": "ok", "argv": ["true"]},
      {"name": "rootcmd", "admin": true, "argv": ["systemctl", "restart", "x"]},
      {"name": "rootgrp", "admin": true, "adminUnlessGroup": "docker", "argv": ["docker", "ps"]}
    ],
    "http": [
      {"name": "docker", "socket": "/var/run/docker.sock", "headers": [],
       "rules": [{"methods": ["POST"], "path": "/containers/[a-z0-9_.%-]+/start"}, {"methods": ["POST"], "path": "/containers/create"}]}
    ],
    "jobs": [
      {"name": "poll", "params": [{"name": "dir", "pattern": "/opt/stacks/[a-z0-9_.-]+"}, {"name": "tag", "pattern": "[a-z0-9.]+", "default": "latest"}],
       "webhook": {"params": ["tag"]},
       "steps": [
        {"id": "fetch", "command": "fetch", "args": ["{param.dir}"]},
        {"id": "local", "command": "rev", "args": ["{param.dir}", "HEAD"]},
        {"id": "remote", "command": "rev", "args": ["{param.dir}", "@{u}"]},
        {"id": "pull", "if": {"step": "remote", "when": "differs", "other": "local"}, "command": "pull", "args": ["{param.dir}"]},
        {"id": "tell", "if": {"step": "pull", "when": "ok"}, "notify": {"title": "Updated {param.dir}", "body": "{step.remote.stdout} {param.tag}", "level": "success"}}
       ]},
      {"name": "simple", "steps": [{"id": "a", "command": "ok"}]},
      {"name": "slowjob", "steps": [{"id": "a", "command": "slow"}]},
      {"name": "limited", "timeoutSec": 1, "steps": [{"id": "a", "command": "slow"}]},
      {"name": "failing", "steps": [{"id": "a", "command": "boom"}, {"id": "b", "command": "ok"}]},
      {"name": "handled", "steps": [
        {"id": "a", "command": "boom", "continueOnError": true},
        {"id": "b", "if": {"step": "a", "when": "failed"}, "notify": {"title": "a failed ({step.a.exitCode})"}},
        {"id": "c", "if": {"step": "a", "when": "ok"}, "command": "ok"}
      ]},
      {"name": "changes", "steps": [{"id": "r", "command": "rev", "args": ["/opt/stacks/x", "HEAD"]}, {"id": "n", "if": {"step": "r", "when": "changed"}, "command": "ok"}]},
      {"name": "rooty", "steps": [{"id": "a", "command": "rootcmd"}, {"id": "b", "command": "ok"}]},
      {"name": "grp", "steps": [{"id": "a", "command": "rootgrp"}]},
      {"name": "http", "params": [{"name": "img", "pattern": "[a-zA-Z0-9_./\"-]+"}],
       "steps": [{"id": "s", "http": {"api": "docker", "method": "POST", "path": "/containers/{param.img}/start", "body": "{\"Image\":\"{param.img}\"}", "json": true}}]}
    ]
  },
  "contributes": {"pages": [], "widgets": [], "snippets": []},
  "visibleTo": {"groups": []}
}`

type call struct {
	admin   bool
	command string
	args    []string
	http    *plugins.HTTPParams
}

type fakeExec struct {
	mu    sync.Mutex
	calls []call
	// out gives a command's stdout and exit code.
	out func(command string, args []string) (string, int)
	// httpStatus is the answer of every HTTP call.
	httpStatus int
}

func (f *fakeExec) Exec(ctx context.Context, admin bool, p plugins.ExecParams) (*plugins.ExecResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{admin: admin, command: p.Command, args: p.Args})
	f.mu.Unlock()
	if p.Command == "slow" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	out, code := "", 0
	if f.out != nil {
		out, code = f.out(p.Command, p.Args)
	} else if p.Command == "boom" {
		code = 1
	}
	return &plugins.ExecResult{Stdout: out, ExitCode: code}, nil
}

func (f *fakeExec) HTTP(ctx context.Context, admin bool, p plugins.HTTPParams) (*plugins.HTTPResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{admin: admin, http: &p})
	f.mu.Unlock()
	st := f.httpStatus
	if st == 0 {
		st = 200
	}
	return &plugins.HTTPResult{Status: st, Body: "{}"}, nil
}

func (f *fakeExec) Close() {}

func (f *fakeExec) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.command)
	}
	return out
}

type fixture struct {
	t      *testing.T
	m      *Manager
	env    *Env
	exec   *fakeExec
	now    time.Time
	nowMu  sync.Mutex
	man    *plugins.Manifest
	accts  map[string]*account.Account
	mu     sync.Mutex
	alerts []notify.Message
	notes  []notify.Message
	dir    string
	// ownerBad makes OwnerOK refuse (by name).
	ownerBad map[string]string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	man, err := plugins.ParseManifest([]byte(testManifest))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, exec: &fakeExec{}, now: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC), man: man, dir: t.TempDir(), ownerBad: map[string]string{},
		accts: map[string]*account.Account{
			"alice": {Name: "alice", UID: 1000, GroupNames: []string{"alice", "wheel"}},
			"bob":   {Name: "bob", UID: 1001, GroupNames: []string{"bob"}},
			"dave":  {Name: "dave", UID: 1002, GroupNames: []string{"dave", "docker"}},
		}}
	f.m = f.manager(f.dir)
	return f
}

func (f *fixture) manager(dir string) *Manager {
	env := Env{
		Dir: dir,
		Now: func() time.Time { f.nowMu.Lock(); defer f.nowMu.Unlock(); return f.now },
		Manifest: func(id string) (*plugins.Manifest, error) {
			if id != "jt" {
				return nil, rpc.Errorf(rpc.NotFound, "There is no plugin %q.", id)
			}
			return f.man, nil
		},
		Account: func(name string) (*account.Account, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if a, ok := f.accts[name]; ok {
				c := *a
				return &c, nil
			}
			return nil, os.ErrNotExist
		},
		OwnerOK:     func(a *account.Account) string { f.mu.Lock(); defer f.mu.Unlock(); return f.ownerBad[a.Name] },
		NewExecutor: func(a *account.Account) Executor { return f.exec },
		Notify: func(ctx context.Context, plugin string, msg notify.Message) error {
			f.mu.Lock()
			f.notes = append(f.notes, msg)
			f.mu.Unlock()
			return nil
		},
		Alert: func(msg notify.Message) { f.mu.Lock(); f.alerts = append(f.alerts, msg); f.mu.Unlock() },
	}
	f.env = &env
	m, err := NewManager(env)
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func (f *fixture) advance(d time.Duration) time.Time {
	f.nowMu.Lock()
	defer f.nowMu.Unlock()
	f.now = f.now.Add(d)
	return f.now
}

func (f *fixture) caller(name string) Caller {
	a := f.accts[name]
	g := groupSet(a)
	return Caller{Name: name, UID: a.UID, Groups: g, CanSudo: a.CanSudo(), IsRoot: a.IsRoot()}
}

func (f *fixture) create(user, job string, params map[string]string, sch *Schedule) *InstanceView {
	f.t.Helper()
	v, err := f.m.Create(f.caller(user), CreateReq{Plugin: "jt", Job: job, Params: params, Schedule: sch})
	if err != nil {
		f.t.Fatalf("create %s: %v", job, err)
	}
	return v
}

func lastRun(f *fixture, id string) Run {
	f.t.Helper()
	h := f.m.History(id, 1)
	if len(h) == 0 {
		f.t.Fatal("no runs")
	}
	return h[0]
}

func stepStatuses(r Run) string {
	var out []string
	for _, s := range r.Steps {
		out = append(out, s.ID+":"+s.Status)
	}
	return strings.Join(out, " ")
}

// ---- schedule ----

func TestScheduleNextWithFakeClock(t *testing.T) {
	utc := time.UTC
	at := func(y int, mo time.Month, d, h, mi int) time.Time { return time.Date(y, mo, d, h, mi, 0, 0, utc) }
	mon := at(2026, 3, 2, 10, 0) // a Monday

	daily := &Schedule{At: []string{"03:30", "15:00"}}
	for _, c := range []struct{ after, want time.Time }{
		{mon, at(2026, 3, 2, 15, 0)},
		{at(2026, 3, 2, 15, 0), at(2026, 3, 3, 3, 30)},
		{at(2026, 3, 2, 3, 29), at(2026, 3, 2, 3, 30)},
		{at(2026, 12, 31, 23, 59), at(2027, 1, 1, 3, 30)},
	} {
		if got := daily.Next(c.after, c.after); !got.Equal(c.want) {
			t.Errorf("daily after %v: got %v want %v", c.after, got, c.want)
		}
	}
	weekly, err := (&Schedule{At: []string{"08:00"}, Days: []int{5, 1}}).Validate() // Monday and Friday
	if err != nil {
		t.Fatal(err)
	}
	if got := weekly.Next(mon, mon); !got.Equal(at(2026, 3, 6, 8, 0)) {
		t.Errorf("weekly from Monday 10:00: %v", got)
	}
	if got := weekly.Next(at(2026, 3, 6, 8, 0), mon); !got.Equal(at(2026, 3, 9, 8, 0)) {
		t.Errorf("weekly after Friday 08:00: %v", got)
	}
	every := &Schedule{Every: 90}
	if got := every.Next(mon, mon); !got.Equal(mon.Add(90 * time.Second)) {
		t.Errorf("interval: %v", got)
	}
}

func TestScheduleValidate(t *testing.T) {
	for name, s := range map[string]*Schedule{
		"interval below a minute": {Every: 59},
		"both":                    {Every: 60, At: []string{"01:00"}},
		"neither":                 {},
		"bad time":                {At: []string{"25:00"}},
		"bad time 2":              {At: []string{"7:00"}},
		"bad day":                 {At: []string{"01:00"}, Days: []int{7}},
		"days with interval":      {Every: 60, Days: []int{1}},
	} {
		if _, err := s.Validate(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if s, err := (&Schedule{At: []string{"09:00", "09:00", "01:00"}, Days: []int{0, 1, 2, 3, 4, 5, 6}}).Validate(); err != nil || len(s.At) != 2 || s.At[0] != "01:00" || s.Days != nil {
		t.Errorf("normalise: %+v %v", s, err)
	}
	if s, _ := (*Schedule)(nil).Validate(); s != nil {
		t.Error("nil stays nil")
	}
}

func TestSchedulerFiresOncePerInterval(t *testing.T) {
	f := newFixture(t)
	in := f.create("alice", "simple", nil, &Schedule{Every: 60})
	t0 := f.now
	f.m.Tick(f.advance(59 * time.Second))
	f.m.Wait()
	if len(f.m.History(in.ID, 10)) != 0 {
		t.Fatal("ran too early")
	}
	f.m.Tick(f.advance(time.Second)) // t0+60s
	f.m.Wait()
	if h := f.m.History(in.ID, 10); len(h) != 1 || h[0].Trigger != "schedule" || h[0].Status != "ok" {
		t.Fatalf("first run: %+v", h)
	}
	f.m.Tick(f.advance(time.Second)) // +61s: nothing
	f.m.Wait()
	if len(f.m.History(in.ID, 10)) != 1 {
		t.Fatal("ran twice in one interval")
	}
	f.m.Tick(f.advance(59 * time.Second)) // t0+120s
	f.m.Wait()
	if len(f.m.History(in.ID, 10)) != 2 {
		t.Fatal("second interval did not run")
	}
	// A daemon that was down for an hour runs the instance once, not 60 times.
	f2 := f.manager(f.dir)
	f.advance(time.Hour)
	f2.Tick(f.now)
	f2.Wait()
	f2.Tick(f.now)
	f2.Wait()
	if n := len(f2.History(in.ID, 20)); n != 3 {
		t.Fatalf("after the restart there should be one catch-up run (3 in all), got %d", n)
	}
	_ = t0
}

func TestSchedulerTimesOfDay(t *testing.T) {
	f := newFixture(t)
	in := f.create("alice", "simple", nil, &Schedule{At: []string{"10:30"}})
	f.m.Tick(f.advance(29 * time.Minute))
	f.m.Wait()
	if len(f.m.History(in.ID, 10)) != 0 {
		t.Fatal("ran too early")
	}
	f.m.Tick(f.advance(time.Minute))
	f.m.Wait()
	if len(f.m.History(in.ID, 10)) != 1 {
		t.Fatal("did not run at 10:30")
	}
	f.m.Tick(f.advance(23 * time.Hour))
	f.m.Wait()
	if len(f.m.History(in.ID, 10)) != 1 {
		t.Fatal("ran before the next 10:30")
	}
	f.m.Tick(f.advance(time.Hour))
	f.m.Wait()
	if len(f.m.History(in.ID, 10)) != 2 {
		t.Fatal("did not run the next day")
	}
	// Switched off: nothing runs.
	off := false
	if _, err := f.m.Update(f.caller("alice"), UpdateReq{ID: in.ID, Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	f.m.Tick(f.advance(48 * time.Hour))
	f.m.Wait()
	if len(f.m.History(in.ID, 10)) != 2 {
		t.Fatal("a disabled instance ran")
	}
}

// ---- running ----

func TestGitPollFlowWithConditions(t *testing.T) {
	f := newFixture(t)
	heads := map[string]string{"HEAD": "aaa111", "@{u}": "aaa111"}
	f.exec.out = func(cmd string, args []string) (string, int) {
		if cmd == "rev" {
			return heads[args[1]] + "\n", 0
		}
		return "", 0
	}
	in := f.create("alice", "poll", map[string]string{"dir": "/opt/stacks/web"}, nil)
	if _, err := f.m.RunNow(f.caller("alice"), "jt", in.ID); err != nil {
		t.Fatal(err)
	}
	f.m.Wait()
	r := lastRun(f, in.ID)
	if r.Status != "ok" || stepStatuses(r) != "fetch:ok local:ok remote:ok pull:skipped tell:skipped" {
		t.Fatalf("nothing new: %s %s", r.Status, stepStatuses(r))
	}
	if got := strings.Join(f.exec.names(), ","); got != "fetch,rev,rev" {
		t.Fatalf("calls: %s", got)
	}
	// The remote moves: pull, then notify.
	heads["@{u}"] = "bbb222"
	f.exec.calls = nil
	if _, err := f.m.RunNow(f.caller("alice"), "jt", in.ID); err != nil {
		t.Fatal(err)
	}
	f.m.Wait()
	r = lastRun(f, in.ID)
	if r.Status != "ok" || stepStatuses(r) != "fetch:ok local:ok remote:ok pull:ok tell:ok" {
		t.Fatalf("update: %s %s", r.Status, stepStatuses(r))
	}
	if got := strings.Join(f.exec.names(), ","); got != "fetch,rev,rev,pull" {
		t.Fatalf("calls: %s", got)
	}
	if len(f.notes) != 1 || f.notes[0].Title != "Updated /opt/stacks/web" || f.notes[0].Body != "bbb222 latest" || f.notes[0].Level != "success" {
		t.Fatalf("notification: %+v", f.notes)
	}
	for _, c := range f.exec.calls {
		if c.admin {
			t.Error("a user job must not use the root bridge")
		}
	}
}

func TestChangedCondition(t *testing.T) {
	f := newFixture(t)
	out := "one\n"
	f.exec.out = func(cmd string, args []string) (string, int) { return out, 0 }
	in := f.create("alice", "changes", nil, nil)
	run := func() Run {
		if _, err := f.m.RunNow(f.caller("alice"), "jt", in.ID); err != nil {
			t.Fatal(err)
		}
		f.m.Wait()
		return lastRun(f, in.ID)
	}
	if s := stepStatuses(run()); s != "r:ok n:ok" {
		t.Fatalf("first run counts as changed: %s", s)
	}
	if s := stepStatuses(run()); s != "r:ok n:skipped" {
		t.Fatalf("same output: %s", s)
	}
	out = "two\n"
	if s := stepStatuses(run()); s != "r:ok n:ok" {
		t.Fatalf("new output: %s", s)
	}
	// The hashes survive a restart.
	f2 := f.manager(f.dir)
	if _, err := f2.RunNow(f.caller("alice"), "jt", in.ID); err != nil {
		t.Fatal(err)
	}
	f2.Wait()
	if s := stepStatuses(f2.History(in.ID, 1)[0]); s != "r:ok n:skipped" {
		t.Fatalf("after restart: %s", s)
	}
}

func TestFailureStopsUnlessContinued(t *testing.T) {
	f := newFixture(t)
	bad := f.create("alice", "failing", nil, nil)
	f.m.RunNow(f.caller("alice"), "jt", bad.ID)
	f.m.Wait()
	r := lastRun(f, bad.ID)
	if r.Status != "failed" || stepStatuses(r) != "a:failed" || !strings.Contains(r.Error, "Step a failed") {
		t.Fatalf("%s %s %q", r.Status, stepStatuses(r), r.Error)
	}
	ok := f.create("alice", "handled", nil, nil)
	f.m.RunNow(f.caller("alice"), "jt", ok.ID)
	f.m.Wait()
	r = lastRun(f, ok.ID)
	if r.Status != "ok" || stepStatuses(r) != "a:failed b:ok c:skipped" || !r.Steps[0].Handled {
		t.Fatalf("handled: %s %s", r.Status, stepStatuses(r))
	}
	if len(f.notes) != 1 || f.notes[0].Title != "a failed (1)" {
		t.Fatalf("notes: %+v", f.notes)
	}
}

func TestAlertsOnFailureAndRecoveryOnly(t *testing.T) {
	f := newFixture(t)
	in := f.create("alice", "failing", nil, nil)
	run := func() {
		f.m.RunNow(f.caller("alice"), "jt", in.ID)
		f.m.Wait()
	}
	run()
	run()
	if len(f.alerts) != 1 || f.alerts[0].Level != "error" || !strings.Contains(f.alerts[0].Title, "Job failed") {
		t.Fatalf("two failures, one alert: %+v", f.alerts)
	}
	// Make the job succeed: replace the failing command's behaviour.
	f.exec.out = func(cmd string, args []string) (string, int) { return "", 0 }
	run()
	if len(f.alerts) != 2 || f.alerts[1].Level != "success" {
		t.Fatalf("recovery alert: %+v", f.alerts)
	}
}

func TestOneRunAtATime(t *testing.T) {
	f := newFixture(t)
	in := f.create("alice", "slowjob", nil, &Schedule{Every: 60})
	if _, err := f.m.RunNow(f.caller("alice"), "jt", in.ID); err != nil {
		t.Fatal(err)
	}
	// A manual run while one runs is refused; a scheduled one is skipped.
	if _, err := f.m.RunNow(f.caller("alice"), "jt", in.ID); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second manual run: %v", err)
	}
	f.m.Tick(f.advance(2 * time.Minute))
	// Webhook calls queue one run behind it, and coalesce.
	_, h := NewToken()
	_ = h
	wh, err := f.m.CreateWebhook(f.caller("alice"), "jt", in.ID, "ci")
	if err != nil {
		t.Fatal(err)
	}
	token := wh.Path[strings.LastIndex(wh.Path, "/")+1:]
	r1 := f.m.HandleHook("jt", token, "1.2.3.4", nil, nil)
	r2 := f.m.HandleHook("jt", token, "1.2.3.4", nil, nil)
	if r1.Status != 202 || r2.Status != 202 || r1.Run != r2.Run {
		t.Fatalf("queued webhook: %+v %+v", r1, r2)
	}
	hist := f.m.History(in.ID, 10)
	if len(hist) != 1 || (hist[0].Status != "running" && hist[0].Status != "queued") {
		t.Fatalf("one run in progress: %+v", hist)
	}
	// Cancelling the first run lets the queued webhook run start; cancel that too.
	done := make(chan struct{})
	go func() { f.m.Wait(); close(done) }()
loop:
	for {
		f.m.CancelAll()
		select {
		case <-done:
			break loop
		case <-time.After(20 * time.Millisecond):
		}
	}
	hist = f.m.History(in.ID, 10)
	found := false
	for _, h := range hist {
		if h.ID == r1.Run && h.Trigger == "webhook" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the queued webhook run %s did not happen: %+v", r1.Run, hist)
	}
}

func TestTimeout(t *testing.T) {
	f := newFixture(t)
	in := f.create("alice", "limited", nil, nil)
	f.m.RunNow(f.caller("alice"), "jt", in.ID)
	start := time.Now()
	f.m.Wait()
	if time.Since(start) > 10*time.Second {
		t.Fatal("the timeout did not end the run")
	}
	r := lastRun(f, in.ID)
	if r.Status != "timeout" || !strings.Contains(r.Error, "longer than 1 seconds") {
		t.Fatalf("%s %q", r.Status, r.Error)
	}
}

func TestHistoryKeepsLastRunsAndIsPrivate(t *testing.T) {
	f := newFixture(t)
	in := f.create("alice", "simple", nil, nil)
	for i := 0; i < KeepRuns+5; i++ {
		f.advance(time.Second)
		f.m.RunNow(f.caller("alice"), "jt", in.ID)
		f.m.Wait()
	}
	if n := len(f.m.History(in.ID, 100)); n != KeepRuns {
		t.Fatalf("history holds %d runs, want %d", n, KeepRuns)
	}
	fi, err := os.Stat(filepath.Join(f.dir, "runs", in.ID+".json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("runs file: %v %v", err, fi)
	}
	fi, err = os.Stat(filepath.Join(f.dir, "instances.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("instances file: %v %v", err, fi)
	}
	if _, err := f.m.Runs(f.caller("bob"), "jt", in.ID, 5); err == nil {
		t.Fatal("another user must not read the logs")
	}
	if _, err := f.m.Runs(f.caller("alice"), "jt", in.ID, 5); err != nil {
		t.Fatal(err)
	}
	admin := f.caller("bob")
	admin.Admin = true
	if _, err := f.m.Runs(admin, "jt", in.ID, 5); err != nil {
		t.Fatalf("an administrator reads every log: %v", err)
	}
	if err := f.m.Delete(f.caller("bob"), "jt", in.ID); err == nil {
		t.Fatal("another user must not delete it")
	}
	if got := f.m.List(f.caller("bob"), "jt", ""); len(got) != 0 {
		t.Fatalf("bob sees %d instances", len(got))
	}
	if got := f.m.List(admin, "", ""); len(got) != 1 {
		t.Fatalf("an administrator sees all: %d", len(got))
	}
}

func TestLogsAreCapped(t *testing.T) {
	f := newFixture(t)
	f.exec.out = func(cmd string, args []string) (string, int) { return strings.Repeat("x", 100<<10), 0 }
	in := f.create("alice", "simple", nil, nil)
	f.m.RunNow(f.caller("alice"), "jt", in.ID)
	f.m.Wait()
	s := lastRun(f, in.ID).Steps[0]
	if len(s.Stdout) > stdoutKeep+8 || !s.Truncated {
		t.Fatalf("stdout %d bytes, truncated %v", len(s.Stdout), s.Truncated)
	}
}

// ---- params and templates ----

func TestParamsAreValidatedAndEscaped(t *testing.T) {
	f := newFixture(t)
	if _, err := f.m.Create(f.caller("alice"), CreateReq{Plugin: "jt", Job: "poll", Params: map[string]string{"dir": "/etc"}}); err == nil {
		t.Fatal("a value off the pattern must be refused")
	}
	if _, err := f.m.Create(f.caller("alice"), CreateReq{Plugin: "jt", Job: "poll"}); err == nil {
		t.Fatal("a missing required param must be refused")
	}
	// A JSON-hostile value cannot break out of the body, and a path value is escaped.
	in := f.create("alice", "http", map[string]string{"img": `a"b/c`}, nil)
	f.m.RunNow(f.caller("alice"), "jt", in.ID)
	f.m.Wait()
	r := lastRun(f, in.ID)
	// "/" is escaped as %2F, which the HTTP layer refuses: the step fails safely.
	if r.Status != "failed" && r.Status != "ok" {
		t.Fatal(r.Status)
	}
	if len(f.exec.calls) != 1 {
		t.Fatalf("calls: %d", len(f.exec.calls))
	}
	c := f.exec.calls[0].http
	if c.Path != "/containers/a%22b%2Fc/start" {
		t.Errorf("path not escaped: %s", c.Path)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(c.Body), &body); err != nil || body["Image"] != `a"b/c` {
		t.Errorf("body not valid JSON with the value intact: %q %v", c.Body, err)
	}
	_ = url.PathEscape
}

// ---- admin ----

func TestAdminInstanceNeedsApprovalByAnAdministrator(t *testing.T) {
	f := newFixture(t)
	// bob cannot administer: refused.
	if _, err := f.m.Create(f.caller("bob"), CreateReq{Plugin: "jt", Job: "rooty", ConfirmAdmin: true}); err == nil || !strings.Contains(err.Error(), "administrator") {
		t.Fatalf("non-admin: %v", err)
	}
	// alice (wheel) must confirm explicitly.
	_, err := f.m.Create(f.caller("alice"), CreateReq{Plugin: "jt", Job: "rooty"})
	var re *rpc.Error
	if err == nil || !asRPC(err, &re) || re.Data == nil {
		t.Fatalf("without confirmation: %v", err)
	}
	v, err := f.m.Create(f.caller("alice"), CreateReq{Plugin: "jt", Job: "rooty", ConfirmAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if !v.NeedsAdmin || v.Approval == nil || v.Approval.By != "alice" || !v.Approval.Valid {
		t.Fatalf("approval: %+v", v)
	}
	f.m.RunNow(f.caller("alice"), "jt", v.ID)
	f.m.Wait()
	if r := lastRun(f, v.ID); r.Status != "ok" || !r.Steps[0].Admin || r.Steps[1].Admin {
		t.Fatalf("admin step routing: %+v", r.Steps)
	}
	if !f.exec.calls[0].admin || f.exec.calls[1].admin {
		t.Fatal("only the admin step goes through the root bridge")
	}
}

func asRPC(err error, out **rpc.Error) bool {
	re, ok := err.(*rpc.Error)
	if ok {
		*out = re
	}
	return ok
}

func TestInstanceDisabledWhenOwnerLosesAdmin(t *testing.T) {
	f := newFixture(t)
	v, err := f.m.Create(f.caller("alice"), CreateReq{Plugin: "jt", Job: "rooty", ConfirmAdmin: true, Schedule: &Schedule{Every: 60}})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.accts["alice"].GroupNames = []string{"alice"} // removed from wheel
	f.mu.Unlock()
	f.m.Reconcile()
	got, _ := f.m.Get(f.caller("alice"), "jt", v.ID)
	if got.Enabled || !strings.Contains(got.DisabledReason, "can no longer administer") {
		t.Fatalf("not disabled: %+v", got)
	}
	f.m.Tick(f.advance(5 * time.Minute))
	f.m.Wait()
	if len(f.exec.calls) != 0 {
		t.Fatal("a disabled instance ran its admin step")
	}
	// Switching it on again fails while alice is not an administrator.
	on := true
	if _, err := f.m.Update(f.caller("alice"), UpdateReq{ID: v.ID, Enabled: &on}); err == nil {
		t.Fatal("enabling must fail")
	}
	f.mu.Lock()
	f.accts["alice"].GroupNames = []string{"alice", "wheel"}
	f.mu.Unlock()
	if _, err := f.m.Update(f.caller("alice"), UpdateReq{ID: v.ID, Enabled: &on}); err != nil {
		t.Fatalf("enable after regaining admin: %v", err)
	}
}

func TestRunRefusedWhenAdminLostBetweenReconciles(t *testing.T) {
	f := newFixture(t)
	v, _ := f.m.Create(f.caller("alice"), CreateReq{Plugin: "jt", Job: "rooty", ConfirmAdmin: true})
	f.mu.Lock()
	f.accts["alice"].GroupNames = []string{"alice"}
	f.mu.Unlock()
	f.m.RunNow(f.caller("alice"), "jt", v.ID)
	f.m.Wait()
	r := lastRun(f, v.ID)
	if r.Status != "failed" || len(f.exec.calls) != 0 {
		t.Fatalf("run %s, calls %d", r.Status, len(f.exec.calls))
	}
	if got, _ := f.m.Get(f.caller("alice"), "jt", v.ID); got.Enabled {
		t.Fatal("the failed check must disable the instance")
	}
}

func TestPluginUpdateChangingAdminStepsNeedsNewApproval(t *testing.T) {
	f := newFixture(t)
	v, _ := f.m.Create(f.caller("alice"), CreateReq{Plugin: "jt", Job: "rooty", ConfirmAdmin: true})
	// The plugin is updated: the admin command now runs something else.
	f.man.Capabilities.Commands[6].Argv = []string{"rm", "-rf", "/"}
	f.m.RunNow(f.caller("alice"), "jt", v.ID)
	f.m.Wait()
	r := lastRun(f, v.ID)
	if r.Status != "failed" || !strings.Contains(r.Error, "approve it again") || len(f.exec.calls) != 0 {
		t.Fatalf("%s %q calls=%d", r.Status, r.Error, len(f.exec.calls))
	}
}

func TestAdminUnlessGroupMembersRunAsThemselves(t *testing.T) {
	f := newFixture(t)
	// dave is in docker: no root bridge, no approval needed.
	v, err := f.m.Create(f.caller("dave"), CreateReq{Plugin: "jt", Job: "grp"})
	if err != nil || v.NeedsAdmin {
		t.Fatalf("%v %+v", err, v)
	}
	f.m.RunNow(f.caller("dave"), "jt", v.ID)
	f.m.Wait()
	if f.exec.calls[0].admin {
		t.Fatal("a docker-group member runs the step as themselves")
	}
	// bob is not: he needs an administrator.
	if _, err := f.m.Create(f.caller("bob"), CreateReq{Plugin: "jt", Job: "grp"}); err == nil {
		t.Fatal("bob needs administrator rights for it")
	}
}

func TestRunAsIsTheCreator(t *testing.T) {
	f := newFixture(t)
	if _, err := f.m.Create(f.caller("bob"), CreateReq{Plugin: "jt", Job: "simple", RunAs: "alice"}); err == nil {
		t.Fatal("running as someone else must be refused")
	}
	v, err := f.m.Create(f.caller("bob"), CreateReq{Plugin: "jt", Job: "simple", RunAs: "bob"})
	if err != nil || v.Owner != "bob" {
		t.Fatalf("%v %+v", err, v)
	}
}

func TestOwnerGoneDisablesInstance(t *testing.T) {
	f := newFixture(t)
	v := f.create("bob", "simple", nil, &Schedule{Every: 60})
	f.mu.Lock()
	f.ownerBad["bob"] = "the account is locked"
	f.mu.Unlock()
	f.m.Reconcile()
	got, _ := f.m.Get(f.caller("bob"), "jt", v.ID)
	if got.Enabled || !strings.Contains(got.DisabledReason, "locked") {
		t.Fatalf("%+v", got)
	}
	f.mu.Lock()
	delete(f.accts, "bob")
	f.ownerBad["bob"] = ""
	f.mu.Unlock()
	w := f.create("alice", "simple", nil, nil)
	_ = w
}

// ---- persistence ----

func TestInstancesSurviveARestart(t *testing.T) {
	f := newFixture(t)
	v := f.create("alice", "poll", map[string]string{"dir": "/opt/stacks/web"}, &Schedule{At: []string{"03:00"}, Days: []int{1}})
	wh, _ := f.m.CreateWebhook(f.caller("alice"), "jt", v.ID, "ci")
	f2 := f.manager(f.dir)
	got := f2.List(f.caller("alice"), "jt", "")
	if len(got) != 1 || got[0].ID != v.ID || got[0].Params["dir"] != "/opt/stacks/web" || got[0].Schedule == nil || got[0].Schedule.At[0] != "03:00" || len(got[0].Webhooks) != 1 {
		t.Fatalf("%+v", got)
	}
	token := wh.Path[strings.LastIndex(wh.Path, "/")+1:]
	if r := f2.HandleHook("jt", token, "9.9.9.9", nil, nil); r.Status != 202 {
		t.Fatalf("webhook after restart: %+v", r)
	}
	f2.Wait()
}

// ---- webhooks ----

func hookFixture(t *testing.T) (*fixture, *InstanceView, string) {
	f := newFixture(t)
	v := f.create("alice", "poll", map[string]string{"dir": "/opt/stacks/web"}, nil)
	wh, err := f.m.CreateWebhook(f.caller("alice"), "jt", v.ID, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(wh.Path, "/hooks/jt/") || len(wh.Token) != 43 {
		t.Fatalf("webhook: %+v", wh)
	}
	return f, v, wh.Token
}

func TestWebhookTokenHandling(t *testing.T) {
	f, v, token := hookFixture(t)
	// Only a hash is stored.
	raw, _ := os.ReadFile(filepath.Join(f.dir, "instances.json"))
	if strings.Contains(string(raw), token) || !strings.Contains(string(raw), HashToken(token)) {
		t.Fatal("the token must not be stored, only its hash")
	}
	if r := f.m.HandleHook("jt", token, "1.1.1.1", nil, nil); r.Status != 202 || r.Run == "" {
		t.Fatalf("valid token: %+v", r)
	}
	f.m.Wait()
	if h := f.m.History(v.ID, 1); len(h) != 1 || h[0].Trigger != "webhook" {
		t.Fatalf("history: %+v", h)
	}
	for name, c := range map[string][2]string{
		"wrong token":  {"jt", token[:42] + "x"},
		"wrong plugin": {"other", token},
		"empty":        {"jt", ""},
	} {
		if r := f.m.HandleHook(c[0], c[1], "2.2.2.2", nil, nil); r.Status != 404 || r.Run != "" {
			t.Errorf("%s: %+v", name, r)
		}
	}
	// A disabled instance answers like an unknown token.
	off := false
	f.m.Update(f.caller("alice"), UpdateReq{ID: v.ID, Enabled: &off})
	if r := f.m.HandleHook("jt", token, "3.3.3.3", nil, nil); r.Status != 404 {
		t.Fatalf("disabled: %+v", r)
	}
}

func TestWebhookRateLimitPerToken(t *testing.T) {
	f, v, token := hookFixture(t)
	f.exec.out = func(string, []string) (string, int) { return "x", 0 }
	for i := 0; i < hookBurst; i++ {
		if r := f.m.HandleHook("jt", token, "1.1.1.1", nil, nil); r.Status != 202 {
			t.Fatalf("call %d: %+v", i, r)
		}
		f.m.Wait()
	}
	r := f.m.HandleHook("jt", token, "1.1.1.1", nil, nil)
	if r.Status != 429 || r.RetryAfter <= 0 {
		t.Fatalf("over the limit: %+v", r)
	}
	// Another token is not affected, the same one recovers with time.
	w2, _ := f.m.CreateWebhook(f.caller("alice"), "jt", v.ID, "other")
	if r := f.m.HandleHook("jt", w2.Token, "1.1.1.1", nil, nil); r.Status != 202 {
		t.Fatalf("second token: %+v", r)
	}
	f.m.Wait()
	f.advance(11 * time.Second)
	if r := f.m.HandleHook("jt", token, "1.1.1.1", nil, nil); r.Status != 202 {
		t.Fatalf("after the refill: %+v", r)
	}
	f.m.Wait()
}

func TestWebhookMissesAreSlowedPerClient(t *testing.T) {
	f, _, token := hookFixture(t)
	for i := 0; i < missBurst; i++ {
		if r := f.m.HandleHook("jt", "nope", "8.8.8.8", nil, nil); r.Status != 404 {
			t.Fatalf("miss %d: %+v", i, r)
		}
	}
	if r := f.m.HandleHook("jt", "nope", "8.8.8.8", nil, nil); r.Status != 429 {
		t.Fatalf("a client that keeps guessing is slowed down: %+v", r)
	}
	if r := f.m.HandleHook("jt", token, "9.9.9.9", nil, nil); r.Status != 202 {
		t.Fatalf("another client is fine: %+v", r)
	}
	f.m.Wait()
}

func TestWebhookRegenerateAndRevoke(t *testing.T) {
	f, v, token := hookFixture(t)
	hookID := f.m.List(f.caller("alice"), "jt", "")[0].Webhooks[0].ID
	n, err := f.m.RegenerateWebhook(f.caller("alice"), "jt", v.ID, hookID)
	if err != nil || n.Token == token {
		t.Fatalf("%v %+v", err, n)
	}
	if r := f.m.HandleHook("jt", token, "1.1.1.1", nil, nil); r.Status != 404 {
		t.Fatalf("the old URL must stop working: %+v", r)
	}
	if r := f.m.HandleHook("jt", n.Token, "1.1.1.1", nil, nil); r.Status != 202 {
		t.Fatalf("the new URL works: %+v", r)
	}
	f.m.Wait()
	if err := f.m.RevokeWebhook(f.caller("bob"), "jt", v.ID, hookID); err == nil {
		t.Fatal("only the owner or an administrator revokes")
	}
	if err := f.m.RevokeWebhook(f.caller("alice"), "jt", v.ID, hookID); err != nil {
		t.Fatal(err)
	}
	f.advance(time.Minute)
	if r := f.m.HandleHook("jt", n.Token, "1.1.1.1", nil, nil); r.Status != 404 {
		t.Fatalf("revoked: %+v", r)
	}
	for i := 0; i < MaxWebhooks; i++ {
		if _, err := f.m.CreateWebhook(f.caller("alice"), "jt", v.ID, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.m.CreateWebhook(f.caller("alice"), "jt", v.ID, ""); err == nil {
		t.Fatal("webhook count must be capped")
	}
}

func TestWebhookParamsAreLimitedAndValidated(t *testing.T) {
	f, v, token := hookFixture(t)
	f.exec.out = func(string, []string) (string, int) { return "x", 0 }
	// "tag" is declared in webhook.params; "dir" is not and is ignored.
	r := f.m.HandleHook("jt", token, "1.1.1.1", []byte(`{"tag":"v2.1","dir":"/etc","extra":1}`), nil)
	if r.Status != 202 {
		t.Fatalf("%+v", r)
	}
	f.m.Wait()
	run := f.m.History(v.ID, 1)[0]
	if run.Status != "ok" {
		t.Fatalf("%+v", run)
	}
	for _, c := range f.exec.calls {
		for _, a := range c.args {
			if a == "/etc" {
				t.Fatal("a param the job does not list was taken from the webhook")
			}
		}
	}
	// A value off the pattern is refused (400) and nothing runs.
	f.exec.calls = nil
	r = f.m.HandleHook("jt", token, "1.1.1.1", []byte(`{"tag":"V2; rm -rf /"}`), nil)
	if r.Status != 400 || len(f.exec.calls) != 0 {
		t.Fatalf("%+v calls=%d", r, len(f.exec.calls))
	}
	// The query string works too.
	r = f.m.HandleHook("jt", token, "1.1.1.1", nil, url.Values{"tag": {"v3"}})
	if r.Status != 202 {
		t.Fatalf("%+v", r)
	}
	f.m.Wait()
}
