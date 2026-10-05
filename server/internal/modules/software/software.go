// Package software implements the bridge methods of the Software section (software.*).
//
// A Backend abstracts one package manager (pacman with an AUR helper, apt,
// dnf, zypper, flatpak). See docs/api/software.md for the methods.
package software

import (
	"context"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Register adds the software.* methods to the registry.
func Register(r *rpc.Registry) {
	r.Handle("software.summary", rpc.User, summary)
	r.Handle("software.check", rpc.User, check)
	r.Handle("software.updates", rpc.User, updates)
	r.Handle("software.installed", rpc.User, installed)
	r.Handle("software.info", rpc.User, info)
	r.Handle("software.search", rpc.User, search)
	r.Handle("software.suggest", rpc.User, suggest)
	r.Handle("software.apps", rpc.User, apps)
	r.Handle("software.icon", rpc.User, icon)
	r.Handle("software.history", rpc.User, history)
	r.Handle("software.status", rpc.User, status)
	r.Handle("software.schedule", rpc.Admin, schedule)
	r.Stream("software.transaction", rpc.Admin, func(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
		return runTransaction(ctx, c, s, false)
	})
	// Flatpak apps of the user installation need no administrator rights.
	r.Stream("software.transactionUser", rpc.User, func(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
		return runTransaction(ctx, c, s, true)
	})
}

type forceParams struct {
	Force bool `json:"force"`
}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func buildSummary(ctx context.Context, m *manager, force bool) (map[string]any, error) {
	ups, at, warns, err := m.updates(ctx, force)
	if err != nil {
		return nil, err
	}
	var size int64
	counts := map[string]int{KindRepo: 0, KindAUR: 0, KindFlatpak: 0}
	reboot, security := false, 0
	for _, u := range ups {
		counts[u.Kind]++
		if u.Kind != KindAUR {
			size += u.Size
		}
		if hasNote(u, "reboot") {
			reboot = true
		}
		if hasNote(u, "security") {
			security++
		}
	}
	manager := ""
	if m.primary != nil {
		manager = m.primary.Name()
	}
	var sched any
	if at := currentSchedule(); at != "" {
		sched = map[string]any{"at": at}
	}
	busy, _ := readStatus()
	osUpdates := cachedOSUpdates(ctx)
	if warns == nil {
		warns = []string{}
	}
	return map[string]any{
		"manager":       manager,
		"sources":       m.sourceNames(),
		"aurHelper":     aurName(m),
		"aurSupported":  false,
		"updates":       len(ups),
		"counts":        counts,
		"downloadSize":  size,
		"rebootNeeded":  reboot,
		"rebootPending": rebootPending(),
		"security":      security,
		"lastCheck":     ms(at),
		"schedule":      sched,
		"busy":          busy,
		"warnings":      warns,
		"osUpdates":     osUpdates,
	}, nil
}

func aurName(m *manager) string {
	if m.aur != nil {
		return m.aur.Name()
	}
	return ""
}

func summary(ctx context.Context, c *rpc.Call) (any, error) {
	m := getManager()
	if err := m.requireAny(); err != nil {
		return nil, err
	}
	return buildSummary(ctx, m, false)
}

// check refreshes the package metadata (as far as the caller's rights allow)
// and recomputes the updates.
func check(ctx context.Context, c *rpc.Call) (any, error) {
	m := getManager()
	if err := m.requireAny(); err != nil {
		return nil, err
	}
	var warn string
	if m.primary != nil {
		if err := m.primary.Refresh(ctx); err != nil {
			warn = err.Error()
		}
	}
	s, err := buildSummary(ctx, m, true)
	if err != nil {
		return nil, err
	}
	if warn != "" {
		s["warnings"] = append(s["warnings"].([]string), warn)
	}
	return s, nil
}

func updates(ctx context.Context, c *rpc.Call) (any, error) {
	var p forceParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	m := getManager()
	if err := m.requireAny(); err != nil {
		return nil, err
	}
	ups, _, _, err := m.updates(ctx, p.Force)
	if err != nil {
		return nil, err
	}
	return ups, nil
}

func installed(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Filter string `json:"filter"`
		Force  bool   `json:"force"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	m := getManager()
	if err := m.requireAny(); err != nil {
		return nil, err
	}
	pkgs, err := m.installed(ctx, p.Force)
	if err != nil {
		return nil, err
	}
	return filterInstalled(pkgs, p.Filter)
}

func info(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := validNames([]string{p.Name}); err != nil {
		return nil, err
	}
	m := getManager()
	b, err := m.backendFor(p.Source)
	if err != nil {
		return nil, err
	}
	d, err := b.Info(ctx, p.Name)
	if err == nil && d.Source == "" && p.Source != "" {
		d.Source = p.Source
	}
	return d, err
}

func search(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Query string `json:"query"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	m := getManager()
	if err := m.requireAny(); err != nil {
		return nil, err
	}
	return m.search(ctx, p.Query)
}

func suggest(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Category string `json:"category"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	m := getManager()
	if err := m.requireAny(); err != nil {
		return nil, err
	}
	return m.suggest(ctx, p.Category)
}

func apps(ctx context.Context, c *rpc.Call) (any, error) {
	m := getManager()
	v, _, err := m.cache.get("apps", cacheTTL, false, func() (any, error) {
		list := scanApps()
		m.resolveApps(ctx, list)
		return list, nil
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

func icon(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	return loadIcon(p.Name, p.Size)
}

func history(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Limit int `json:"limit"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if p.Limit <= 0 {
		p.Limit = 300
	}
	if p.Limit > 3000 {
		p.Limit = 3000
	}
	return getManager().history(ctx, p.Limit)
}

func status(ctx context.Context, c *rpc.Call) (any, error) {
	busy, tx := readStatus()
	if !busy && lockedByOther(getManager()) {
		busy = true
	}
	return map[string]any{"busy": busy, "transaction": tx}, nil
}

func schedule(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		At *string `json:"at"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	at, err := getManager().setSchedule(ctx, p.At)
	if err != nil {
		return nil, err
	}
	if at == "" {
		return map[string]any{"at": nil}, nil
	}
	return map[string]any{"at": at}, nil
}
