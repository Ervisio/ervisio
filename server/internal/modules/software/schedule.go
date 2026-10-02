package software

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// The scheduled update is a systemd timer pair written to /etc/systemd/system:
//
//	ervisio-update.timer    OnCalendar=*-*-* HH:MM:00, Persistent=true
//	ervisio-update.service  oneshot, runs the same commands as "Update all"
const (
	timerName   = brand.Slug + "-update.timer"
	serviceName = brand.Slug + "-update.service"
)

var systemdDir = "/etc/systemd/system"

var atRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)
var onCalendarRe = regexp.MustCompile(`(?m)^OnCalendar=\*-\*-\* ([0-9]{2}:[0-9]{2}):00$`)

// currentSchedule returns the scheduled time ("03:00") or "".
func currentSchedule() string {
	b, err := os.ReadFile(filepath.Join(systemdDir, timerName))
	if err != nil {
		return ""
	}
	if g := onCalendarRe.FindStringSubmatch(string(b)); g != nil {
		return g[1]
	}
	return ""
}

// systemdQuote quotes one argument of an ExecStart line.
func systemdQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\;$%") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)
	return `"` + r.Replace(s) + `"`
}

// renderService builds the unit that upgrades everything.
func renderService(plans []Plan, optionalFrom int) (string, error) {
	var b strings.Builder
	b.WriteString("# Written by " + brand.Name + " (Software > Update all > schedule). Remove it from the web UI.\n")
	b.WriteString("[Unit]\nDescription=" + brand.Name + " scheduled system update\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=oneshot\n")
	envs := map[string]bool{}
	for _, p := range plans {
		for _, s := range p.Steps {
			for _, e := range s.Env {
				envs[e] = true
			}
		}
	}
	for e := range envs {
		b.WriteString("Environment=" + systemdQuote(e) + "\n")
	}
	for pi, p := range plans {
		for _, s := range p.Steps {
			path, err := sys.LookPath(s.Name)
			if err != nil {
				return "", err
			}
			line := "ExecStart="
			if pi >= optionalFrom {
				line += "-" // a failing Flatpak update must not fail the system update
			}
			line += systemdQuote(path)
			for _, a := range s.Args {
				line += " " + systemdQuote(a)
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String(), nil
}

func renderTimer(at string) string {
	return "# Written by " + brand.Name + ".\n[Unit]\nDescription=" + brand.Name + " scheduled system update\n\n[Timer]\nOnCalendar=*-*-* " + at + ":00\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n"
}

func (m *manager) setSchedule(ctx context.Context, at *string) (string, error) {
	if at == nil || *at == "" {
		_, _ = run(ctx, 30*time.Second, nil, "systemctl", "disable", "--now", timerName)
		_ = os.Remove(filepath.Join(systemdDir, timerName))
		_ = os.Remove(filepath.Join(systemdDir, serviceName))
		_, _ = run(ctx, 30*time.Second, nil, "systemctl", "daemon-reload")
		return "", nil
	}
	if !atRe.MatchString(*at) {
		return "", rpc.Errorf(rpc.Invalid, "The time must look like 03:00 (24-hour clock).")
	}
	if m.primary == nil {
		return "", rpc.Errorf(rpc.Unavailable, "No supported system package manager was found.")
	}
	var plans []Plan
	p, err := m.primary.Upgrade(nil)
	if err != nil {
		return "", err
	}
	plans = append(plans, p)
	optional := len(plans)
	if m.flatpak != nil {
		if fp, err := m.flatpak.(scoped).WithScope("system").Upgrade(nil); err == nil {
			plans = append(plans, fp)
		}
	}
	svc, err := renderService(plans, optional)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(systemdDir, serviceName), []byte(svc), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(systemdDir, timerName), []byte(renderTimer(*at)), 0o644); err != nil {
		return "", err
	}
	if _, err := run(ctx, 30*time.Second, nil, "systemctl", "daemon-reload"); err != nil {
		return "", rpc.Errorf(rpc.Internal, "Could not reload systemd: %v", err)
	}
	if _, err := run(ctx, 30*time.Second, nil, "systemctl", "enable", "--now", timerName); err != nil {
		return "", rpc.Errorf(rpc.Internal, "Could not start the update timer: %v", err)
	}
	return *at, nil
}
