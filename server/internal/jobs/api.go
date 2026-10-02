package jobs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ervisio/ervisio/server/internal/modules/plugins"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Caller is who asks: the signed-in user of a session.
type Caller struct {
	Name   string
	UID    uint32
	Groups map[string]bool
	// Admin: the session has administrator rights now (root, or unlocked).
	// It may see and manage every instance.
	Admin bool
	// CanSudo: the account may administer the machine, so it may approve
	// an instance that runs steps as root.
	CanSudo bool
	IsRoot  bool
}

func (c Caller) owns(in *Instance) bool { return in.Owner == c.Name && in.OwnerUID == c.UID }

// InstanceView is an instance as the API returns it (no token hashes).
type InstanceView struct {
	ID             string            `json:"id"`
	Plugin         string            `json:"plugin"`
	Job            string            `json:"job"`
	Name           string            `json:"name"`
	Params         map[string]string `json:"params"`
	Schedule       *Schedule         `json:"schedule,omitempty"`
	Owner          string            `json:"owner"`
	Enabled        bool              `json:"enabled"`
	DisabledReason string            `json:"disabledReason,omitempty"`
	// NeedsAdmin: the job runs steps as root, so it was approved by an administrator.
	NeedsAdmin bool `json:"needsAdmin"`
	// Approval says who approved it and whether the approval still holds.
	Approval *ApprovalView `json:"approval,omitempty"`
	Webhooks []WebhookView `json:"webhooks"`
	Created  int64         `json:"created"`
	Updated  int64         `json:"updated"`
	Running  bool          `json:"running"`
	// NextRun is the next scheduled start in ms (absent when none).
	NextRun int64       `json:"nextRun,omitempty"`
	Last    *RunSummary `json:"last,omitempty"`
}

// ApprovalView is Approval without the signature.
type ApprovalView struct {
	By    string `json:"by"`
	At    int64  `json:"at"`
	Valid bool   `json:"valid"`
}

// WebhookView is a webhook without its token hash.
type WebhookView struct {
	ID       string `json:"id"`
	Label    string `json:"label,omitempty"`
	Created  int64  `json:"created"`
	LastUsed int64  `json:"lastUsed,omitempty"`
}

// RunSummary is a run without its logs.
type RunSummary struct {
	ID      string `json:"id"`
	Trigger string `json:"trigger"`
	Status  string `json:"status"`
	Started int64  `json:"started"`
	Ended   int64  `json:"ended,omitempty"`
	Error   string `json:"error,omitempty"`
}

func summary(r *Run) *RunSummary {
	return &RunSummary{ID: r.ID, Trigger: r.Trigger, Status: r.Status, Started: r.Started, Ended: r.Ended, Error: r.Error}
}

// view builds the API form of an instance; the caller holds no lock.
func (m *Manager) view(in *Instance) InstanceView {
	v := InstanceView{ID: in.ID, Plugin: in.Plugin, Job: in.Job, Name: in.Name, Params: in.Params, Schedule: in.Schedule, Owner: in.Owner,
		Enabled: in.Enabled, DisabledReason: in.DisabledReason, Webhooks: []WebhookView{}, Created: in.Created, Updated: in.Updated}
	if v.Params == nil {
		v.Params = map[string]string{}
	}
	for _, w := range in.Webhooks {
		v.Webhooks = append(v.Webhooks, WebhookView{ID: w.ID, Label: w.Label, Created: w.Created, LastUsed: w.LastUsed})
	}
	if man, err := m.env.Manifest(in.Plugin); err == nil {
		if job := man.Job(in.Job); job != nil {
			if a, err := m.env.Account(in.Owner); err == nil && a != nil {
				v.NeedsAdmin = man.JobNeedsAdmin(job, a.IsRoot(), groupSet(a))
			} else {
				v.NeedsAdmin = in.Approval != nil
			}
			if in.Approval != nil {
				v.Approval = &ApprovalView{By: in.Approval.By, At: in.Approval.At, Valid: in.Approval.Sig == approvalSig(man, job)}
			}
		}
	} else if in.Approval != nil {
		v.NeedsAdmin = true
		v.Approval = &ApprovalView{By: in.Approval.By, At: in.Approval.At}
	}
	m.mu.Lock()
	if lv := m.live[in.ID]; lv != nil {
		v.Running = lv.running
		if !lv.next.IsZero() && in.Enabled {
			v.NextRun = lv.next.UnixMilli()
		}
	}
	m.mu.Unlock()
	runs := m.readRunsLocked(in.ID)
	if n := len(runs); n > 0 {
		v.Last = summary(&runs[n-1])
	}
	return v
}

func (m *Manager) readRunsLocked(id string) []Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readRuns(id)
}

func notFound() error { return rpc.Errorf(rpc.NotFound, "There is no such job instance.") }

// access finds an instance the caller may use: its owner, or an
// administrator. Anything else is "not found", so ids do not leak.
func (m *Manager) access(c Caller, id, plugin string) (*Instance, error) {
	in, ok := m.find(id)
	if !ok || (plugin != "" && in.Plugin != plugin) || (!c.owns(in) && !c.Admin) {
		return nil, notFound()
	}
	return in, nil
}

// CreateReq is plugins.jobs.create.
type CreateReq struct {
	Plugin   string            `json:"plugin"`
	Job      string            `json:"job"`
	Name     string            `json:"name"`
	Params   map[string]string `json:"params"`
	Schedule *Schedule         `json:"schedule"`
	// RunAs must be empty or the caller: jobs run as the user who creates them.
	RunAs   string `json:"runAs"`
	Enabled *bool  `json:"enabled"`
	// ConfirmAdmin confirms that the instance may run steps as root (needed
	// when the job has admin steps; only an administrator can).
	ConfirmAdmin bool `json:"confirmAdmin"`
}

var errAdminConfirm = func(what string) error {
	return rpc.Errorf(rpc.Invalid, "This job runs steps with administrator rights, without asking for a password each time. %s", what).
		WithData(map[string]string{"reason": "admin_confirmation_required"})
}

// approve checks and records the administrator's approval of an instance
// whose job needs root, or clears it for one that does not. c is who
// approves (the caller); the groups that exempt a user from an admin step
// are the owner's.
func (m *Manager) approve(c Caller, man *plugins.Manifest, job *plugins.JobDef, in *Instance, confirm bool) error {
	groups, root := map[string]bool{}, in.OwnerUID == 0
	if a, err := m.env.Account(in.Owner); err == nil && a != nil {
		groups, root = groupSet(a), a.IsRoot()
	}
	if !man.JobNeedsAdmin(job, root, groups) {
		in.Approval = nil
		return nil
	}
	if !c.CanSudo {
		return rpc.Errorf(rpc.Forbidden, "Only an administrator can set up a job that needs administrator rights.")
	}
	if !confirm {
		return errAdminConfirm("Pass confirmAdmin: true to approve it.")
	}
	in.Approval = &Approval{By: c.Name, At: m.env.Now().UnixMilli(), Sig: approvalSig(man, job)}
	return nil
}

func validLabel(s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > max || strings.ContainsAny(s, "\r\n\x00") {
		return "", rpc.Errorf(rpc.Invalid, "The name can be up to %d characters on one line.", max)
	}
	return s, nil
}

// Create makes a job instance owned by the caller.
func (m *Manager) Create(c Caller, req CreateReq) (*InstanceView, error) {
	man, err := m.env.Manifest(req.Plugin)
	if err != nil {
		return nil, err
	}
	if !man.CanBeUsedBy(c.Groups, c.CanSudo) {
		return nil, rpc.Errorf(rpc.Forbidden, "%s is not available to your account.", man.Name)
	}
	job := man.Job(req.Job)
	if job == nil {
		return nil, rpc.Errorf(rpc.NotFound, "%s does not declare a job %q.", man.Name, req.Job)
	}
	if req.RunAs != "" && req.RunAs != c.Name {
		return nil, rpc.Errorf(rpc.Forbidden, "A job runs as the user who creates it. Leave runAs out, or use your own user name.")
	}
	name, err := validLabel(req.Name, 60)
	if err != nil {
		return nil, err
	}
	params, perr := job.ResolveParams(req.Params)
	if perr != nil {
		return nil, rpc.Errorf(rpc.Invalid, "%v", perr)
	}
	sched, serr := req.Schedule.Validate()
	if serr != nil {
		return nil, rpc.Errorf(rpc.Invalid, "%v", serr)
	}
	now := m.env.Now()
	in := &Instance{ID: randHex(4), Plugin: req.Plugin, Job: req.Job, Name: name, Params: params, Schedule: sched, Owner: c.Name, OwnerUID: c.UID,
		Enabled: req.Enabled == nil || *req.Enabled, Webhooks: []Webhook{}, Created: now.UnixMilli(), Updated: now.UnixMilli()}
	if err := m.approve(c, man, job, in, req.ConfirmAdmin); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if len(m.list) >= MaxInstances {
		m.mu.Unlock()
		return nil, rpc.Errorf(rpc.Unavailable, "This server already has %d job instances. Delete some first.", MaxInstances)
	}
	n := 0
	for _, o := range m.list {
		if o.Plugin == req.Plugin {
			n++
		}
	}
	if n >= MaxPerPlugin {
		m.mu.Unlock()
		return nil, rpc.Errorf(rpc.Unavailable, "%s already has %d job instances. Delete some first.", man.Name, MaxPerPlugin)
	}
	m.list = append(m.list, in)
	lv := &live{}
	m.live[in.ID] = lv
	m.reschedule(in, lv, now)
	err = m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		return nil, rpc.Errorf(rpc.Internal, "Could not save the job instance: %v", err)
	}
	v := m.view(in)
	return &v, nil
}

// List returns the instances the caller may see: their own, or every one
// for an administrator. plugin and job narrow the list ("" = any).
func (m *Manager) List(c Caller, plugin, job string) []InstanceView {
	m.mu.Lock()
	var snap []Instance
	for _, in := range m.list {
		if (plugin != "" && in.Plugin != plugin) || (job != "" && in.Job != job) || (!c.owns(in) && !c.Admin) {
			continue
		}
		snap = append(snap, *in)
	}
	m.mu.Unlock()
	out := make([]InstanceView, 0, len(snap))
	for i := range snap {
		out = append(out, m.view(&snap[i]))
	}
	return out
}

// Get returns one instance.
func (m *Manager) Get(c Caller, plugin, id string) (*InstanceView, error) {
	in, err := m.access(c, id, plugin)
	if err != nil {
		return nil, err
	}
	v := m.view(in)
	return &v, nil
}

// optSchedule tells "not given" from "null" (clear the schedule).
type optSchedule struct {
	Set   bool
	Value *Schedule
}

// UnmarshalJSON is called only when the key is present, null included.
func (o *optSchedule) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	o.Value = &Schedule{}
	return json.Unmarshal(b, o.Value)
}

// UpdateReq is plugins.jobs.update: only the fields given change.
type UpdateReq struct {
	Plugin       string             `json:"plugin"`
	ID           string             `json:"id"`
	Name         *string            `json:"name"`
	Params       *map[string]string `json:"params"`
	Schedule     optSchedule        `json:"schedule"`
	Enabled      *bool              `json:"enabled"`
	ConfirmAdmin bool               `json:"confirmAdmin"`
}

// Update changes an instance: name, params, schedule, enabled. New params
// of a job that needs root need the administrator's approval again.
func (m *Manager) Update(c Caller, req UpdateReq) (*InstanceView, error) {
	cur, err := m.access(c, req.ID, req.Plugin)
	if err != nil {
		return nil, err
	}
	in := *cur
	man, merr := m.env.Manifest(in.Plugin)
	var job *plugins.JobDef
	if merr == nil {
		job = man.Job(in.Job)
	}
	if req.Name != nil {
		if in.Name, err = validLabel(*req.Name, 60); err != nil {
			return nil, err
		}
	}
	if req.Params != nil {
		if job == nil {
			return nil, rpc.Errorf(rpc.Unavailable, "The plugin no longer declares this job.")
		}
		params, perr := job.ResolveParams(*req.Params)
		if perr != nil {
			return nil, rpc.Errorf(rpc.Invalid, "%v", perr)
		}
		in.Params = params
		// The administrator approves what the job will do with the new values.
		if err := m.approve(c, man, job, &in, req.ConfirmAdmin); err != nil {
			return nil, err
		}
	}
	if req.Schedule.Set {
		if in.Schedule, err = req.Schedule.Value.Validate(); err != nil {
			return nil, rpc.Errorf(rpc.Invalid, "%v", err)
		}
	}
	if req.Enabled != nil && *req.Enabled && !cur.Enabled {
		// Switching on again re-checks the owner and the approval.
		if reason, disable := m.runnable(&in); reason != "" && disable {
			return nil, rpc.Errorf(rpc.Invalid, "This job instance cannot be switched on: %s", reason)
		}
	}
	m.mu.Lock()
	stored := m.findLocked(in.ID)
	if stored == nil {
		m.mu.Unlock()
		return nil, notFound()
	}
	stored.Name, stored.Params, stored.Schedule, stored.Approval = in.Name, in.Params, in.Schedule, in.Approval
	if req.Enabled != nil {
		stored.Enabled = *req.Enabled
		if stored.Enabled {
			stored.DisabledReason = ""
		}
	}
	stored.Updated = m.env.Now().UnixMilli()
	m.reschedule(stored, m.live[stored.ID], m.env.Now())
	err = m.saveLocked()
	cp := *stored
	m.mu.Unlock()
	if err != nil {
		return nil, rpc.Errorf(rpc.Internal, "Could not save the job instance: %v", err)
	}
	v := m.view(&cp)
	return &v, nil
}

// Delete removes an instance and its runs, cancelling a run in progress.
func (m *Manager) Delete(c Caller, plugin, id string) error {
	if _, err := m.access(c, id, plugin); err != nil {
		return err
	}
	m.mu.Lock()
	for i, in := range m.list {
		if in.ID == id {
			m.list = append(m.list[:i:i], m.list[i+1:]...)
			break
		}
	}
	if lv := m.live[id]; lv != nil && lv.cancel != nil {
		lv.cancel()
	}
	err := m.saveLocked()
	m.mu.Unlock()
	m.removeRuns(id)
	if err != nil {
		return rpc.Errorf(rpc.Internal, "Could not save the job instances: %v", err)
	}
	return nil
}

// RunNow starts a run at once and returns its id.
func (m *Manager) RunNow(c Caller, plugin, id string) (string, error) {
	if _, err := m.access(c, id, plugin); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	in := m.findLocked(id)
	if in == nil {
		return "", notFound()
	}
	if !in.Enabled {
		return "", rpc.Errorf(rpc.Conflict, "This job instance is switched off.")
	}
	runID, err := m.beginLocked(in, Trigger{Kind: "manual", By: c.Name})
	if err == ErrBusy {
		return "", rpc.Errorf(rpc.Conflict, "This job instance is already running.")
	}
	return runID, err
}

// Runs returns the history of an instance, newest first, with logs.
func (m *Manager) Runs(c Caller, plugin, id string, limit int) ([]Run, error) {
	if _, err := m.access(c, id, plugin); err != nil {
		return nil, err
	}
	return m.History(id, limit), nil
}

// WebhookCreated is the answer to creating or regenerating a webhook. The
// token is shown only here.
type WebhookCreated struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	Token string `json:"token"`
	// Path is /hooks/<plugin>/<token>, to put after the console's address.
	Path string `json:"path"`
}

// CreateWebhook adds a webhook URL to an instance.
func (m *Manager) CreateWebhook(c Caller, plugin, id, label string) (*WebhookCreated, error) {
	if _, err := m.access(c, id, plugin); err != nil {
		return nil, err
	}
	label, err := validLabel(label, 60)
	if err != nil {
		return nil, err
	}
	token, hash := NewToken()
	m.mu.Lock()
	defer m.mu.Unlock()
	in := m.findLocked(id)
	if in == nil {
		return nil, notFound()
	}
	if len(in.Webhooks) >= MaxWebhooks {
		return nil, rpc.Errorf(rpc.Unavailable, "An instance can have %d webhook URLs. Revoke one first.", MaxWebhooks)
	}
	w := Webhook{ID: randHex(4), Label: label, Hash: hash, Created: m.env.Now().UnixMilli()}
	in.Webhooks = append(in.Webhooks, w)
	if err := m.saveLocked(); err != nil {
		return nil, rpc.Errorf(rpc.Internal, "Could not save the webhook: %v", err)
	}
	return &WebhookCreated{ID: w.ID, Label: label, Token: token, Path: "/hooks/" + in.Plugin + "/" + token}, nil
}

// RegenerateWebhook replaces a webhook's token: the old URL stops working.
func (m *Manager) RegenerateWebhook(c Caller, plugin, id, hookID string) (*WebhookCreated, error) {
	if _, err := m.access(c, id, plugin); err != nil {
		return nil, err
	}
	token, hash := NewToken()
	m.mu.Lock()
	defer m.mu.Unlock()
	in := m.findLocked(id)
	if in == nil {
		return nil, notFound()
	}
	for i := range in.Webhooks {
		if in.Webhooks[i].ID == hookID {
			in.Webhooks[i].Hash, in.Webhooks[i].LastUsed, in.Webhooks[i].Created = hash, 0, m.env.Now().UnixMilli()
			if err := m.saveLocked(); err != nil {
				return nil, rpc.Errorf(rpc.Internal, "Could not save the webhook: %v", err)
			}
			return &WebhookCreated{ID: hookID, Label: in.Webhooks[i].Label, Token: token, Path: "/hooks/" + in.Plugin + "/" + token}, nil
		}
	}
	return nil, rpc.Errorf(rpc.NotFound, "There is no such webhook.")
}

// RevokeWebhook removes a webhook URL.
func (m *Manager) RevokeWebhook(c Caller, plugin, id, hookID string) error {
	if _, err := m.access(c, id, plugin); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	in := m.findLocked(id)
	if in == nil {
		return notFound()
	}
	for i := range in.Webhooks {
		if in.Webhooks[i].ID == hookID {
			in.Webhooks = append(in.Webhooks[:i:i], in.Webhooks[i+1:]...)
			if err := m.saveLocked(); err != nil {
				return rpc.Errorf(rpc.Internal, "Could not save the webhook: %v", err)
			}
			return nil
		}
	}
	return rpc.Errorf(rpc.NotFound, "There is no such webhook.")
}

// SetEnabled is Update for the Settings page: switch an instance on or off.
func (m *Manager) SetEnabled(c Caller, id string, on bool) (*InstanceView, error) {
	return m.Update(c, UpdateReq{ID: id, Enabled: &on})
}

// String is for logs.
func (in *Instance) String() string {
	return fmt.Sprintf("%s/%s %s as %s (%s)", in.Plugin, in.Job, in.ID, in.Owner, in.Schedule.Describe())
}
