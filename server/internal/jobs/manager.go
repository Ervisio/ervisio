package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/modules/plugins"
	"github.com/ervisio/ervisio/server/internal/notify"
)

// Executor runs the steps of one run for one user. Exec and HTTP call
// plugins.exec and plugins.http on that user's bridge (admin false) or on a
// root bridge (admin true); Close stops the bridges.
type Executor interface {
	Exec(ctx context.Context, admin bool, p plugins.ExecParams) (*plugins.ExecResult, error)
	HTTP(ctx context.Context, admin bool, p plugins.HTTPParams) (*plugins.HTTPResult, error)
	Close()
}

// Env connects the manager to the daemon. Everything the manager needs from
// outside is here, so tests can replace it.
type Env struct {
	// Dir is the state directory (instances.json, runs/).
	Dir string
	Now func() time.Time
	// Manifest returns the manifest of an enabled, trusted plugin.
	Manifest func(plugin string) (*plugins.Manifest, error)
	// Account looks a user up now.
	Account func(name string) (*account.Account, error)
	// OwnerOK returns "" while the account may still run jobs, else why not
	// (shell, sign-in allow lists, root policy).
	OwnerOK func(a *account.Account) string
	// NewExecutor makes the executor of one run.
	NewExecutor func(a *account.Account) Executor
	// Notify sends a plugin's notification (job notify steps); the daemon
	// applies the plugin's rate limit.
	Notify func(ctx context.Context, plugin string, msg notify.Message) error
	// Alert sends a core notification about a job (failed, recovered).
	Alert func(msg notify.Message)
	Logf  func(format string, args ...any)
}

// live is the in-memory state of an instance.
type live struct {
	next    time.Time
	running bool
	cancel  context.CancelFunc
	run     *Run
	pending *pending
}

// pending is the one webhook call that arrived during a run.
type pending struct {
	runID  string
	params map[string]string
}

// Trigger says why a run starts.
type Trigger struct {
	// Kind is schedule, manual or webhook.
	Kind string
	// By is the user of a manual run.
	By string
	// Params override the instance's params (webhook).
	Params map[string]string
}

// Errors of Start.
var (
	// ErrBusy: a manual run was asked while the instance runs.
	ErrBusy = errors.New("this job instance is already running")
	// errSkipped: a scheduled run found the instance running.
	errSkipped = errors.New("skipped")
)

// Manager owns the job instances.
type Manager struct {
	env Env

	mu   sync.Mutex
	list []*Instance
	live map[string]*live

	sem chan struct{}
	wg  sync.WaitGroup

	hooks hookLimits
}

// NewManager loads the instances from env.Dir.
func NewManager(env Env) (*Manager, error) {
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.Logf == nil {
		env.Logf = func(string, ...any) {}
	}
	m := &Manager{env: env, live: map[string]*live{}, sem: make(chan struct{}, MaxRunning), hooks: newHookLimits()}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) instancesPath() string { return filepath.Join(m.env.Dir, "instances.json") }
func (m *Manager) runsPath(id string) string {
	return filepath.Join(m.env.Dir, "runs", id+".json")
}

type instancesFile struct {
	Version   int         `json:"version"`
	Instances []*Instance `json:"instances"`
}

func (m *Manager) load() error {
	b, err := os.ReadFile(m.instancesPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var f instancesFile
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("%s: %v", m.instancesPath(), err)
	}
	now := m.env.Now()
	for _, in := range f.Instances {
		if in.Params == nil {
			in.Params = map[string]string{}
		}
		m.list = append(m.list, in)
		lv := &live{}
		m.live[in.ID] = lv
		m.reschedule(in, lv, now)
	}
	return nil
}

// writeJSON writes v as a 0600 file, replacing any old one atomically.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.OpenFile(path+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// saveLocked persists the instances; the caller holds m.mu.
func (m *Manager) saveLocked() error {
	return writeJSON(m.instancesPath(), instancesFile{Version: 1, Instances: m.list})
}

func (m *Manager) findLocked(id string) *Instance {
	for _, in := range m.list {
		if in.ID == id {
			return in
		}
	}
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// reschedule computes the next scheduled start of an instance. An interval
// that fell due while the daemon was down is due at once, one run only.
func (m *Manager) reschedule(in *Instance, lv *live, now time.Time) {
	lv.next = time.Time{}
	if !in.Enabled || in.Schedule == nil {
		return
	}
	last := time.UnixMilli(in.LastStart)
	if in.LastStart == 0 {
		last = time.UnixMilli(in.Created)
	}
	lv.next = in.Schedule.Next(now, last)
	if in.Schedule.Every > 0 && lv.next.Before(now) {
		lv.next = now
	}
}

// Run drives the schedule until ctx ends: it starts due instances every
// second and checks owners every minute.
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	tick := 0
	for {
		select {
		case <-ctx.Done():
			m.CancelAll()
			m.wg.Wait()
			return
		case <-t.C:
			m.Tick(m.env.Now())
			if tick++; tick%60 == 30 {
				m.Reconcile()
			}
		}
	}
}

// Wait blocks until every run in progress has ended (tests, shutdown).
func (m *Manager) Wait() { m.wg.Wait() }

// CancelAll cancels the runs in progress.
func (m *Manager) CancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, lv := range m.live {
		if lv.cancel != nil {
			lv.cancel()
		}
	}
}

// Tick starts the runs due at now.
func (m *Manager) Tick(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, in := range m.list {
		lv := m.live[in.ID]
		if lv == nil || lv.next.IsZero() || now.Before(lv.next) || !in.Enabled || in.Schedule == nil {
			continue
		}
		lv.next = in.Schedule.Next(now, now)
		if _, err := m.beginLocked(in, Trigger{Kind: "schedule"}); err != nil && !errors.Is(err, errSkipped) {
			m.env.Logf("jobs: %s: %v", in.ID, err)
		}
	}
}

// beginLocked starts a run (or queues a webhook call behind a running one).
func (m *Manager) beginLocked(in *Instance, tr Trigger) (string, error) {
	lv := m.live[in.ID]
	if lv.running {
		switch tr.Kind {
		case "manual":
			return "", ErrBusy
		case "webhook":
			if lv.pending == nil {
				lv.pending = &pending{runID: "r" + randHex(5), params: tr.Params}
			}
			return lv.pending.runID, nil
		}
		return "", errSkipped
	}
	return m.startLocked(in, lv, tr, "r"+randHex(5)), nil
}

func (m *Manager) startLocked(in *Instance, lv *live, tr Trigger, runID string) string {
	now := m.env.Now()
	run := &Run{ID: runID, Instance: in.ID, Trigger: tr.Kind, By: tr.By, Started: now.UnixMilli(), Status: "queued", Steps: []StepRun{}}
	ctx, cancel := context.WithCancel(context.Background())
	lv.running, lv.run, lv.cancel = true, run, cancel
	in.LastStart = now.UnixMilli()
	_ = m.saveLocked()
	snapshot := *in
	m.wg.Add(1)
	go m.execute(ctx, &snapshot, run, tr)
	return runID
}

// finish is called when a run ends: it records the run, updates the
// instance and starts the queued webhook run, if any.
func (m *Manager) finish(in *Instance, run *Run, outputs map[string]string, status string) (prev string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lv := m.live[in.ID]
	cur := m.findLocked(in.ID)
	if lv != nil {
		lv.running, lv.cancel, lv.run = false, nil, nil
	}
	if cur == nil {
		return "" // deleted while running
	}
	prev = cur.LastStatus
	if status == "ok" || status == "failed" || status == "timeout" {
		cur.LastStatus = status
	}
	if outputs != nil {
		cur.Outputs = outputs
	}
	_ = m.saveLocked()
	m.appendRun(cur.ID, run)
	if lv != nil {
		m.reschedule(cur, lv, m.env.Now())
		if p := lv.pending; p != nil {
			lv.pending = nil
			if cur.Enabled {
				m.startLocked(cur, lv, Trigger{Kind: "webhook", Params: p.params}, p.runID)
			}
		}
	}
	return prev
}

// appendRun adds a finished run to the instance's file, keeping the last KeepRuns.
func (m *Manager) appendRun(id string, run *Run) {
	runs := m.readRuns(id)
	runs = append(runs, *run)
	if len(runs) > KeepRuns {
		runs = runs[len(runs)-KeepRuns:]
	}
	if err := writeJSON(m.runsPath(id), runs); err != nil {
		m.env.Logf("jobs: could not save the runs of %s: %v", id, err)
	}
}

func (m *Manager) readRuns(id string) []Run {
	var runs []Run
	if b, err := os.ReadFile(m.runsPath(id)); err == nil {
		_ = json.Unmarshal(b, &runs)
	}
	return runs
}

// History returns the last runs of an instance, newest first, with the
// run in progress first.
func (m *Manager) History(id string, limit int) []Run {
	if limit <= 0 || limit > KeepRuns {
		limit = KeepRuns
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Run
	if lv := m.live[id]; lv != nil && lv.run != nil {
		r := *lv.run
		r.Steps = append([]StepRun{}, lv.run.Steps...)
		out = append(out, r)
	}
	runs := m.readRuns(id)
	for i := len(runs) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, runs[i])
	}
	if out == nil {
		out = []Run{}
	}
	return out
}

// disableLocked switches an instance off and says why.
func (m *Manager) disableLocked(in *Instance, reason string) {
	in.Enabled = false
	in.DisabledReason = reason
	in.Updated = m.env.Now().UnixMilli()
	if lv := m.live[in.ID]; lv != nil {
		lv.next = time.Time{}
	}
	_ = m.saveLocked()
	m.env.Logf("jobs: %s (%s/%s of %s) disabled: %s", in.ID, in.Plugin, in.Job, in.Owner, reason)
}

// Reconcile switches off the instances whose owner can no longer run them:
// the account is gone or may not sign in, or an instance that needs
// administrator rights lost its owner's admin rights.
func (m *Manager) Reconcile() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, in := range m.list {
		if !in.Enabled {
			continue
		}
		if reason, disable := m.runnable(in); disable {
			m.disableLocked(in, reason)
		}
	}
}

func groupSet(a *account.Account) map[string]bool {
	g := map[string]bool{}
	for _, n := range a.GroupNames {
		g[n] = true
	}
	return g
}

// runnable checks whether the instance may run now. It returns a reason
// when not; disable says the reason is permanent (the daemon switches the
// instance off) rather than a failure of this run (a plugin turned off).
func (m *Manager) runnable(in *Instance) (reason string, disable bool) {
	a, err := m.env.Account(in.Owner)
	if err != nil || a == nil {
		return fmt.Sprintf("The account %s no longer exists.", in.Owner), true
	}
	if a.UID != in.OwnerUID {
		return fmt.Sprintf("The account %s is not the one that created this job.", in.Owner), true
	}
	if why := m.env.OwnerOK(a); why != "" {
		return fmt.Sprintf("%s may no longer run jobs: %s", in.Owner, why), true
	}
	man, err := m.env.Manifest(in.Plugin)
	if err != nil {
		return err.Error(), false
	}
	job := man.Job(in.Job)
	if job == nil {
		return fmt.Sprintf("%s no longer declares the job %s.", man.Name, in.Job), false
	}
	if !man.CanBeUsedBy(groupSet(a), a.CanSudo()) {
		return fmt.Sprintf("%s is not available to %s.", man.Name, in.Owner), false
	}
	if man.JobNeedsAdmin(job, a.IsRoot(), groupSet(a)) {
		if !a.CanSudo() {
			return fmt.Sprintf("%s can no longer administer this machine, and the job needs administrator rights.", in.Owner), true
		}
		if in.Approval == nil || in.Approval.Sig != approvalSig(man, job) {
			return fmt.Sprintf("%s changed what this job does as an administrator. An administrator has to approve it again.", man.Name), true
		}
	}
	return "", false
}

// find returns a copy of the instance.
func (m *Manager) find(id string) (*Instance, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	in := m.findLocked(id)
	if in == nil {
		return nil, false
	}
	c := *in
	return &c, true
}

// nextRun returns the next scheduled start (zero if none).
func (m *Manager) nextRun(id string) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lv := m.live[id]; lv != nil {
		return lv.next
	}
	return time.Time{}
}

func (m *Manager) removeRuns(id string) { _ = os.Remove(m.runsPath(id)) }
