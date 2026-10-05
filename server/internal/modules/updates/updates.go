// Package updates exposes Ervisio's self-update to the Settings section:
// updates.check and updates.status (any user), updates.apply (admin,
// stream) and updates.rollback (admin). The work is done by
// internal/update; see docs/api/updates.md.
package updates

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	cfg "github.com/ervisio/ervisio/server/internal/config"
	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/update"
)

// DaemonDev is set by the bridge's --dev flag: a daemon in --dev never
// updates itself.
var DaemonDev bool

// Package state, replaceable by tests.
var (
	checker    = update.NewChecker()
	newUpdater = func() *update.Updater { return update.New(checker) }
	executable = func() string {
		exe, err := os.Executable()
		if err != nil {
			return ""
		}
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			return r
		}
		return exe
	}
)

// Register adds the updates.* methods.
func Register(r *rpc.Registry) {
	r.Handle("updates.check", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			Force bool `json:"force"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return check(ctx, p.Force)
	})
	r.Handle("updates.status", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return status(), nil
	})
	r.Stream("updates.apply", rpc.Admin, func(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
		var p struct {
			Version string `json:"version"`
		}
		if err := c.Bind(&p); err != nil {
			return err
		}
		return apply(ctx, p.Version, s)
	})
	r.Handle("updates.rollback", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			Version string `json:"version"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return rollback(ctx, p.Version)
	})
}

// Settings mirrors the [updates] configuration.
type Settings struct {
	Channel       string `json:"channel"`
	AutoCheck     bool   `json:"autoCheck"`
	AutoInstall   bool   `json:"autoInstall"`
	AutoInstallAt string `json:"autoInstallAt"`
}

func settings() Settings {
	c, _, _, err := cfg.Load(configmod.Path)
	if err != nil || c == nil {
		c = cfg.Default()
	}
	return Settings{Channel: c.Updates.Channel, AutoCheck: c.Updates.AutoCheck,
		AutoInstall: c.Updates.AutoInstall, AutoInstallAt: c.Updates.AutoInstallAt}
}

// Latest describes the newest release of the channel.
type Latest struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	Name        string `json:"name,omitempty"`
	PublishedAt int64  `json:"publishedAt"` // unix ms
	Notes       string `json:"notes"`       // markdown, render as text
	URL         string `json:"url,omitempty"`
	Prerelease  bool   `json:"prerelease"`
	Asset       string `json:"asset"`
	// Size of the archive for this machine (0: no build for this architecture).
	Size int64 `json:"size"`
}

// CheckResult is the answer of updates.check.
type CheckResult struct {
	Current   string  `json:"current"`
	Channel   string  `json:"channel"`
	AutoCheck bool    `json:"autoCheck"`
	CheckedAt int64   `json:"checkedAt"`
	Latest    *Latest `json:"latest"`
	Newer     bool    `json:"newer"`
	Arch      string  `json:"arch"`
	// ManagedBy names the package manager that installs updates of a
	// packaged Ervisio ("" = self-update).
	ManagedBy string `json:"managedBy,omitempty"`
}

func check(ctx context.Context, force bool) (*CheckResult, error) {
	st := settings()
	res := &CheckResult{Current: brand.Version, Channel: st.Channel, AutoCheck: st.AutoCheck, Arch: update.Arch,
		ManagedBy: newUpdater().Layout.ManagedBy()}
	rel, at, err := checker.Latest(ctx, st.Channel, force)
	res.CheckedAt = at.UnixMilli()
	switch {
	case errors.Is(err, update.ErrNoRelease):
		return res, nil
	case err != nil:
		return nil, rpc.Errorf(rpc.Unavailable, "%v", err)
	}
	v := rel.Version()
	l := &Latest{Version: v, Tag: rel.Tag, Name: rel.Name, PublishedAt: rel.PublishedAt.UnixMilli(),
		Notes: rel.Body, URL: rel.HTMLURL, Prerelease: rel.Prerelease, Asset: update.ArchiveName(v, update.Arch)}
	if a := rel.Asset(l.Asset); a != nil {
		l.Size = a.Size
	}
	res.Latest = l
	res.Newer = update.Newer(v, brand.Version)
	return res, nil
}

// Status is the answer of updates.status.
type Status struct {
	Current string `json:"current"`
	// Install: versioned | flat | none
	Install   string `json:"install"`
	CanUpdate bool   `json:"canUpdate"`
	Reason    string `json:"reason,omitempty"`
	// Previous is the version kept for rollback ("" = none).
	Previous    string         `json:"previous"`
	Installed   []string       `json:"installed"`
	Last        *update.Result `json:"last"`
	Running     bool           `json:"running"`
	PackageBusy bool           `json:"packageBusy"`
	Settings    Settings       `json:"settings"`
	Arch        string         `json:"arch"`
	// ManagedBy: see CheckResult.
	ManagedBy string `json:"managedBy,omitempty"`
}

func status() *Status {
	u := newUpdater()
	ok, reason := u.Supported(executable(), DaemonDev)
	s := &Status{
		Current:     brand.Version,
		Install:     u.Layout.Kind(),
		CanUpdate:   ok,
		Reason:      reason,
		Installed:   u.Layout.Installed(),
		Last:        u.State.ReadLast(),
		PackageBusy: u.State.PackageTransactionRunning(),
		Settings:    settings(),
		Arch:        update.Arch,
		ManagedBy:   u.Layout.ManagedBy(),
	}
	if s.Installed == nil {
		s.Installed = []string{}
	}
	if s.Install == update.KindVersioned {
		s.Previous = u.Layout.Previous()
	}
	s.Running = s.Last != nil && s.Last.State == update.StateRunning
	return s
}

func apply(ctx context.Context, want string, s rpc.Stream) error {
	u := newUpdater()
	if ok, reason := u.Supported(executable(), DaemonDev); !ok {
		return rpc.Errorf(rpc.Unavailable, "%s", reason)
	}
	// The download may take a while: keep going if the browser goes away
	// (closing the tab must not leave a half-installed version), but
	// bound the whole operation.
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
	defer cancel()
	_, _, err := u.Apply(work, settings().Channel, want, false, func(ev update.Event) {
		if ctx.Err() == nil {
			_ = s.Send(ev)
		}
	})
	return mapErr(err)
}

func rollback(ctx context.Context, want string) (any, error) {
	u := newUpdater()
	if ok, reason := u.Supported(executable(), DaemonDev); !ok {
		return nil, rpc.Errorf(rpc.Unavailable, "%s", reason)
	}
	v, unit, err := u.Rollback(ctx, want)
	if err != nil {
		return nil, mapErr(err)
	}
	return map[string]string{"version": v, "unit": unit}, nil
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, update.ErrBusy), errors.Is(err, update.ErrPackages), errors.Is(err, update.ErrChanged), errors.Is(err, update.ErrUpToDate):
		return rpc.Errorf(rpc.Conflict, "%v", err)
	case errors.Is(err, update.ErrNoPrevious), errors.Is(err, update.ErrNoRelease):
		return rpc.Errorf(rpc.NotFound, "%v", err)
	case errors.Is(err, update.ErrNotInstalled), errors.Is(err, update.ErrNoBuild), errors.Is(err, update.ErrManaged), errors.Is(err, update.ErrWindows):
		return rpc.Errorf(rpc.Unavailable, "%v", err)
	}
	var re *rpc.Error
	if errors.As(err, &re) {
		return re
	}
	return rpc.Errorf(rpc.Internal, "%v", err)
}
