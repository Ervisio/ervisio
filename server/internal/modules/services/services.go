// Package services implements the bridge methods of the Services section (services.*).
//
// On Linux it talks to systemd through `systemctl` and `journalctl` (argv, no
// shell, no D-Bus); on Windows it drives the Service Control Manager. The
// backends live in services_unix.go and services_windows.go and answer with the
// shapes below. Methods are documented in docs/api/services.md.
package services

import (
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Unit is one row of services.list.
type Unit struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Load        string   `json:"load"`
	Active      string   `json:"active"`
	Sub         string   `json:"sub"`
	State       string   `json:"state"`   // running | failed | stopped | finished
	Enabled     string   `json:"enabled"` // enabled | disabled | static | masked | ... ("" unknown)
	Memory      *uint64  `json:"memory"`  // bytes, null when not accounted
	CPUNs       *uint64  `json:"cpuNs"`   // cumulative CPU time
	PID         int      `json:"pid"`
	Since       int64    `json:"since"` // unix seconds of the last state change, 0 unknown
	Purpose     string   `json:"purpose"`
	Next        int64    `json:"next,omitempty"`     // timers: next run, unix seconds
	Last        int64    `json:"last,omitempty"`     // timers: last run
	Triggers    string   `json:"triggers,omitempty"` // timers/sockets: the unit they start
	Listen      []string `json:"listen,omitempty"`   // sockets
}

// FailedUnit is an entry of services.summary.failed.
type FailedUnit struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Since       int64  `json:"since"`
	Result      string `json:"result"`
	ExitCode    int    `json:"exitCode"`
	Hint        string `json:"hint,omitempty"`
	HintID      string `json:"hintId,omitempty"`
}

// LogLine is one journal entry.
type LogLine struct {
	Time     int64  `json:"time"` // unix milliseconds
	Priority int    `json:"priority"`
	Message  string `json:"message"`
}

type listParams struct {
	Type string `json:"type"`
	// Fresh skips the short-lived cache of unit file states (use after enable/disable).
	Fresh bool `json:"fresh"`
}

type nameParams struct {
	Name string `json:"name"`
}

type actionParams struct {
	Name   string `json:"name"`
	Action string `json:"action"`
}

type saveParams struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type logsParams struct {
	Name  string `json:"name"`
	Lines int    `json:"lines"`
}

var depKeys = []struct{ key, prop string }{
	{"requires", "Requires"}, {"wants", "Wants"}, {"after", "After"}, {"before", "Before"},
	{"requiredBy", "RequiredBy"}, {"wantedBy", "WantedBy"}, {"conflicts", "Conflicts"},
	{"boundBy", "BoundBy"}, {"partOf", "PartOf"}, {"triggers", "Triggers"}, {"triggeredBy", "TriggeredBy"},
}

// Register adds the services.* methods to the registry.
func Register(r *rpc.Registry) {
	r.Handle("services.list", rpc.User, handleList)
	r.Handle("services.summary", rpc.User, handleSummary)
	r.Handle("services.get", rpc.User, handleGet)
	r.Handle("services.action", rpc.Admin, handleAction)
	r.Handle("services.unitFile", rpc.User, handleUnitFile)
	r.Handle("services.saveOverride", rpc.Admin, handleSaveOverride)
	r.Handle("services.logs", rpc.User, handleLogs)
	r.Stream("services.watch", rpc.User, handleWatch)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func unitTypeParam(s string) (string, error) {
	switch s {
	case "":
		return "service", nil
	case "service", "timer", "socket":
		return s, nil
	}
	return "", rpc.Errorf(rpc.Invalid, "type must be service, timer or socket.")
}
