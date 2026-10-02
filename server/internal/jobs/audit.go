package jobs

import (
	"github.com/ervisio/ervisio/server/internal/audit"
)

// The jobs record to the activity log through audit.Record (the daemon sets
// the default log; tools and tests have none, so nothing is written).

func jobLabel(in *Instance) string {
	if in.Name != "" {
		return in.Name
	}
	return in.Job
}

// auditOrigin says what started a run: "job <name>" for the schedule and
// manual runs, "webhook" for a webhook call.
func auditOrigin(in *Instance, tr Trigger) string {
	if tr.Kind == "webhook" {
		return "webhook"
	}
	return "job " + jobLabel(in)
}

func auditResult(ok bool) string {
	if ok {
		return audit.OK
	}
	return audit.Failed
}

// auditStep records a command or an HTTP call a job step made, as its owner.
func (m *Manager) auditStep(in *Instance, tr Trigger, action, via, target string, admin bool, code *int, ok bool, msg string) {
	detail := "job " + jobLabel(in) + "/" + in.ID
	if msg != "" {
		detail += ": " + msg
	}
	audit.Record(audit.Entry{User: in.Owner, Source: audit.SourcePlugin, Plugin: in.Plugin, Action: action,
		Via: via, Target: target, Result: auditResult(ok), Code: code, Admin: admin, Detail: detail, Origin: auditOrigin(in, tr)})
}

// auditRunEnd records the end of a run: who started it and how it ended.
func (m *Manager) auditRunEnd(in *Instance, run *Run, tr Trigger) {
	res := auditResult(run.Status == "ok")
	detail := "run " + run.ID + ", " + run.Status
	if tr.By != "" {
		detail += ", started by " + tr.By
	}
	if run.Error != "" {
		detail += ": " + run.Error
	}
	audit.Record(audit.Entry{User: in.Owner, Source: audit.SourcePlugin, Plugin: in.Plugin, Action: "job.run",
		Target: in.Job + " " + jobLabel(in), Result: res, Detail: detail, Origin: auditOrigin(in, tr)})
}

// auditWebhook records an accepted webhook call.
func (m *Manager) auditWebhook(in *Instance, hookID, ip, runID string) {
	audit.Record(audit.Entry{User: in.Owner, IP: ip, Source: audit.SourcePlugin, Plugin: in.Plugin, Action: "job.webhook",
		Target: in.Job + " " + jobLabel(in), Result: audit.OK, Detail: "webhook " + hookID + ", run " + runID, Origin: "webhook"})
}

// auditApproval records an administrator's approval of a job that needs
// administrator rights.
func (m *Manager) auditApproval(approver string, in *Instance) {
	audit.Record(audit.Entry{User: approver, Source: audit.SourcePlugin, Plugin: in.Plugin, Action: "job.approve", Admin: true,
		Target: in.Job + " " + jobLabel(in), Result: audit.OK, Detail: "runs as " + in.Owner})
}
