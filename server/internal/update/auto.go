package update

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/config"
)

// Auto runs inside linuxadmind (root): it checks for updates periodically
// (updates.auto_check) and installs them at updates.auto_install_at when
// updates.auto_install is on (never on a packaged install, see
// Layout.ManagedBy). It never starts while a package transaction
// runs (Apply refuses), and retries the next day.
type Auto struct {
	Updater *Updater
	Config  func() *config.Config
	Logf    func(format string, args ...any)
	Now     func() time.Time
	// CheckEvery is the auto_check period (default 6h).
	CheckEvery time.Duration

	lastCheck   time.Time
	lastNotice  string
	lastInstall string // yyyy-mm-dd of the last automatic attempt
}

func (a *Auto) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Auto) logf(format string, args ...any) {
	if a.Logf != nil {
		a.Logf(format, args...)
	}
}

// Run loops until ctx ends.
func (a *Auto) Run(ctx context.Context) {
	// First check a little after start, not during boot.
	a.lastCheck = a.now().Add(-a.checkEvery() + 2*time.Minute)
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.Tick(ctx)
		}
	}
}

func (a *Auto) checkEvery() time.Duration {
	if a.CheckEvery > 0 {
		return a.CheckEvery
	}
	return 6 * time.Hour
}

// Tick does one round of the scheduler (exported for tests).
func (a *Auto) Tick(ctx context.Context) {
	cfg := a.Config()
	if cfg == nil {
		return
	}
	now := a.now()
	if cfg.Updates.AutoCheck && now.Sub(a.lastCheck) >= a.checkEvery() {
		a.lastCheck = now
		rel, _, err := a.Updater.Checker.Latest(ctx, cfg.Updates.Channel, false)
		switch {
		case errors.Is(err, ErrNoRelease):
		case err != nil:
			a.logf("update check failed: %v", err)
		case Newer(rel.Version(), a.Updater.Current) && rel.Version() != a.lastNotice:
			a.lastNotice = rel.Version()
			a.logf("update available: %s (running %s)", rel.Version(), a.Updater.Current)
		}
	}
	// A package manager installs the updates of a packaged LinuxAdmin.
	if !cfg.Updates.AutoInstall || a.Updater.Layout.ManagedBy() != "" {
		return
	}
	at, err := config.ParseClock(cfg.Updates.AutoInstallAt)
	if err != nil {
		return
	}
	day := now.Format("2006-01-02")
	mins := now.Hour()*60 + now.Minute()
	if a.lastInstall == day || mins < at || mins > at+2 {
		return
	}
	a.lastInstall = day
	ictx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	v, unit, err := a.Updater.Apply(ictx, cfg.Updates.Channel, "", true, nil)
	switch {
	case errors.Is(err, ErrUpToDate), errors.Is(err, ErrNoRelease):
		a.logf("automatic update: already up to date (%s)", a.Updater.Current)
	case err != nil:
		a.logf("automatic update failed: %v", err)
		if werr := a.Updater.State.WriteLast(Result{State: StateFailed, Kind: KindUpdate, From: a.Updater.Current,
			StartedAt: now.UnixMilli(), FinishedAt: nowMs(), Error: fmt.Sprint(err), Auto: true}); werr != nil {
			a.logf("write last.json: %v", werr)
		}
	default:
		a.logf("automatic update to %s started (unit %s); the daemon restarts now", v, unit)
	}
}
