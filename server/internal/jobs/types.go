// Package jobs runs plugin jobs in the background: job instances that
// plugins create at runtime (plugins.jobs.*) from the jobs their manifest
// declares (capabilities.jobs), on a schedule or through a webhook, as the
// user who created them. It lives in the daemon, which keeps the instances
// under its state directory so they survive restarts.
//
// The daemon never runs a step itself: each step goes to the owner's bridge
// (plugins.exec / plugins.http), so it passes exactly the checks a page's
// call would. Steps that need administrator rights go to a root bridge the
// daemon starts without an interactive unlock; that is only done for
// instances an administrator approved (Approval), and only while the owner
// can still administer the machine.
package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/ervisio/ervisio/server/internal/modules/plugins"
)

// Limits.
const (
	// KeepRuns is how many runs of an instance are kept, with their logs.
	KeepRuns = 20
	// MaxInstances bounds all instances, MaxPerPlugin those of one plugin.
	MaxInstances = 200
	MaxPerPlugin = 50
	// MaxWebhooks bounds the webhook URLs of an instance.
	MaxWebhooks = 5
	// MaxRunning is how many runs execute at once; the others wait.
	MaxRunning = 4

	stdoutKeep   = 8 << 10
	stderrKeep   = 4 << 10
	runLogBudget = 64 << 10
	// MinInterval is the shortest schedule interval in seconds.
	MinInterval = 60
)

// Instance is a job instance, as stored.
type Instance struct {
	ID     string            `json:"id"`
	Plugin string            `json:"plugin"`
	Job    string            `json:"job"`
	Name   string            `json:"name,omitempty"`
	Params map[string]string `json:"params"`
	// Schedule is nil for an instance that runs only on demand or through
	// a webhook.
	Schedule *Schedule `json:"schedule,omitempty"`
	// Owner is the user the instance runs as: the one who created it.
	Owner    string `json:"owner"`
	OwnerUID uint32 `json:"ownerUid"`
	Enabled  bool   `json:"enabled"`
	// DisabledReason says why the daemon switched the instance off.
	DisabledReason string `json:"disabledReason,omitempty"`
	// Approval is set for an instance whose job needs administrator rights.
	Approval *Approval `json:"approval,omitempty"`
	Webhooks []Webhook `json:"webhooks"`
	Created  int64     `json:"created"`
	Updated  int64     `json:"updated"`
	// LastStart (ms) is the start of the last run, which paces an interval schedule.
	LastStart int64 `json:"lastStart,omitempty"`
	// LastStatus is the status of the last finished run.
	LastStatus string `json:"lastStatus,omitempty"`
	// Outputs holds the hash of each step's output in the last run
	// (condition "changed").
	Outputs map[string]string `json:"outputs,omitempty"`
}

// Approval records who confirmed that the instance may run steps as root,
// and for which definition (Sig covers the job and the commands and HTTP
// APIs it uses: a plugin update that changes them asks for a new approval).
type Approval struct {
	By  string `json:"by"`
	At  int64  `json:"at"`
	Sig string `json:"sig"`
}

// Webhook is one URL of an instance. Only the SHA-256 of its token is kept.
type Webhook struct {
	ID       string `json:"id"`
	Label    string `json:"label,omitempty"`
	Hash     string `json:"hash"`
	Created  int64  `json:"created"`
	LastUsed int64  `json:"lastUsed,omitempty"`
}

// Run is one execution, with its logs.
type Run struct {
	ID       string `json:"id"`
	Instance string `json:"instance"`
	// Trigger is schedule, manual or webhook.
	Trigger string `json:"trigger"`
	// By is the user who started a manual run.
	By      string `json:"by,omitempty"`
	Started int64  `json:"started"`
	Ended   int64  `json:"ended,omitempty"`
	// Status is queued, running, ok, failed, timeout or cancelled.
	Status string    `json:"status"`
	Error  string    `json:"error,omitempty"`
	Steps  []StepRun `json:"steps"`
}

// StepRun is the log of one step.
type StepRun struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Status is ok, failed or skipped.
	Status   string `json:"status"`
	Started  int64  `json:"started,omitempty"`
	Duration int64  `json:"durationMs,omitempty"`
	// ExitCode of a command; HTTPStatus of an HTTP call.
	ExitCode   *int   `json:"exitCode,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Error      string `json:"error,omitempty"`
	// Truncated: the log was cut to its last part.
	Truncated bool `json:"truncated,omitempty"`
	// Admin: the step ran on the root bridge.
	Admin bool `json:"admin,omitempty"`
	// Handled: the step failed but continueOnError let the job go on.
	Handled bool `json:"handled,omitempty"`
}

// approvalSig identifies what an administrator approved: the job
// definition plus every command and HTTP API it uses.
func approvalSig(m *plugins.Manifest, j *plugins.JobDef) string {
	used := struct {
		Plugin string          `json:"plugin"`
		Job    *plugins.JobDef `json:"job"`
		Cmds   map[string]any  `json:"commands"`
		APIs   map[string]any  `json:"http"`
	}{Plugin: m.ID, Job: j, Cmds: map[string]any{}, APIs: map[string]any{}}
	for i := range j.Steps {
		if n := j.Steps[i].Command; n != "" {
			if c := m.Command(n); c != nil {
				used.Cmds[n] = c
			}
		}
		if h := j.Steps[i].HTTP; h != nil {
			if a := m.API(h.API); a != nil {
				used.APIs[h.API] = a
			}
		}
	}
	b, _ := json.Marshal(used)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
