package server

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/notify"
)

// The Overview page's alerts (a failed service, pending updates, failed SSH
// sign-ins, a full disk or swap: overview.alerts) are computed when a page
// asks. To also send them to the notification channels, the daemon asks a
// bridge for them every few minutes and sends the ones that are new, or got
// worse, to the channels that subscribe to "alerts".

const (
	alertFirstScan = 90 * time.Second
	alertEvery     = 5 * time.Minute
)

// watchedAlert is the part of an Overview alert the watcher needs.
type watchedAlert struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

// alertWatch remembers which alerts were already sent.
type alertWatch struct {
	seen  map[string]string
	first bool
}

func newAlertWatch() *alertWatch { return &alertWatch{seen: map[string]string{}, first: true} }

// fresh returns the alerts to send now: warnings and errors that were not
// there at the last scan, and warnings that became errors. The first scan
// after the daemon starts only records what is there, so a restart does not
// repeat everything. An alert that went away (or turned ok) is forgotten,
// so it is sent again if it comes back.
func (w *alertWatch) fresh(cur []watchedAlert) []watchedAlert {
	var out []watchedAlert
	now := map[string]string{}
	for _, a := range cur {
		if a.Severity != "warn" && a.Severity != "err" {
			continue
		}
		now[a.ID] = a.Severity
		prev := w.seen[a.ID]
		if !w.first && (prev == "" || (prev == "warn" && a.Severity == "err")) {
			out = append(out, a)
		}
	}
	w.seen, w.first = now, false
	return out
}

// readAlerts asks a bridge for overview.alerts: a root bridge when the
// daemon is root (the SSH log needs it), else the daemon's own user (dev).
func (s *Server) readAlerts(ctx context.Context) ([]watchedAlert, error) {
	var p *bridge.Proc
	var err error
	if os.Geteuid() == 0 {
		var ra *account.Account
		if ra, err = account.Lookup("root"); err != nil {
			return nil, err
		}
		p, err = bridge.StartRoot(ctx, s.rootSpec(ra))
	} else {
		p, err = bridge.StartUser(ctx, s.spec(s.devUser, ""))
	}
	if err != nil {
		return nil, err
	}
	defer p.Stop()
	raw, err := p.Call(ctx, "overview.alerts", nil)
	if err != nil {
		return nil, err
	}
	var list []watchedAlert
	return list, json.Unmarshal(raw, &list)
}

// runAlertWatch scans every alertEvery while some channel wants alerts.
func (s *Server) runAlertWatch(ctx context.Context) {
	w := newAlertWatch()
	next := time.NewTimer(alertFirstScan)
	defer next.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-next.C:
		}
		next.Reset(alertEvery)
		if s.notifier == nil || !s.notifier.Subscribed(notify.EventAlerts) || (s.devUser == nil && os.Geteuid() != 0) {
			w.first = true // start from the state at the next scan
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, time.Minute)
		list, err := s.readAlerts(sctx)
		cancel()
		if err != nil {
			s.log.Printf("alert watch: %v", err)
			continue
		}
		for _, a := range w.fresh(list) {
			level := "warn"
			if a.Severity == "err" {
				level = "error"
			}
			s.notifier.Send(ctx, notify.Message{Title: a.Title, Body: a.Detail, Level: level, Source: "Ervisio", Link: "/overview"}, notify.EventAlerts)
		}
	}
}
