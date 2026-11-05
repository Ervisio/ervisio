//go:build windows

// Windows backend: the Service Control Manager through golang.org/x/sys/windows/svc/mgr.
// Rights are enforced by the SCM itself; ERROR_ACCESS_DENIED becomes needs_admin.

package services

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

const (
	readRights   = windows.SERVICE_QUERY_CONFIG | windows.SERVICE_QUERY_STATUS | windows.SERVICE_ENUMERATE_DEPENDENTS
	actionRights = readRights | windows.SERVICE_START | windows.SERVICE_STOP | windows.SERVICE_PAUSE_CONTINUE | windows.SERVICE_CHANGE_CONFIG
)

// scmError converts an error from the SCM into an rpc error.
func scmError(err error, name, what string) error {
	if err == nil {
		return nil
	}
	var re *rpc.Error
	if errors.As(err, &re) {
		return err
	}
	var en windows.Errno
	if errors.As(err, &en) {
		return winError(uint32(en), name, what)
	}
	return err
}

// connect opens the SCM with the least rights needed. Close with Disconnect.
func connect() (*mgr.Mgr, error) {
	access := uint32(windows.SC_MANAGER_CONNECT | windows.SC_MANAGER_ENUMERATE_SERVICE)
	h, err := windows.OpenSCManager(nil, nil, access)
	if err != nil {
		return nil, scmError(err, "", "connect to the service manager for")
	}
	return &mgr.Mgr{Handle: h}, nil
}

func openService(m *mgr.Mgr, name string, access uint32) (*mgr.Service, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "%q is not a valid service name.", name)
	}
	h, err := windows.OpenService(m.Handle, p, access)
	if err != nil {
		return nil, scmError(err, name, "open")
	}
	return &mgr.Service{Name: name, Handle: h}, nil
}

func unitFrom(name string, cfg mgr.Config, st svc.Status) *Unit {
	u := &Unit{Name: name, Description: cfg.Description, Load: "loaded", PID: int(st.ProcessId), Purpose: PurposeOf(name + ".service")}
	if u.Description == "" {
		u.Description = cfg.DisplayName
	} else if cfg.DisplayName != "" && !strings.EqualFold(cfg.DisplayName, name) {
		u.Description = cfg.DisplayName + " - " + u.Description
	}
	u.Active, u.Sub, u.State = winState(uint32(st.State), st.Win32ExitCode)
	u.Enabled = winEnabled(cfg.StartType, cfg.DelayedAutoStart)
	return u
}

// listAll returns every service the caller may read. Services that cannot be
// opened are skipped.
func listAll(m *mgr.Mgr) ([]*Unit, error) {
	names, err := m.ListServices()
	if err != nil {
		return nil, scmError(err, "", "list")
	}
	units := make([]*Unit, 0, len(names))
	for _, n := range names {
		s, err := openService(m, n, readRights)
		if err != nil {
			continue
		}
		cfg, cerr := s.Config()
		st, qerr := s.Query()
		s.Close()
		if cerr != nil || qerr != nil {
			continue
		}
		units = append(units, unitFrom(n, cfg, st))
	}
	sort.Slice(units, func(i, j int) bool { return strings.ToLower(units[i].Name) < strings.ToLower(units[j].Name) })
	return units, nil
}

func listUnits(typ string) ([]*Unit, error) {
	if typ != "service" {
		return []*Unit{}, nil // no timers or sockets on Windows
	}
	m, err := connect()
	if err != nil {
		return nil, err
	}
	defer m.Disconnect()
	return listAll(m)
}

func handleList(ctx context.Context, c *rpc.Call) (any, error) {
	var p listParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	typ, err := unitTypeParam(p.Type)
	if err != nil {
		return nil, err
	}
	units, err := listUnits(typ)
	if err != nil {
		return nil, err
	}
	return map[string]any{"units": units}, nil
}

func handleSummary(ctx context.Context, c *rpc.Call) (any, error) {
	units, err := listUnits("service")
	if err != nil {
		return nil, err
	}
	total, running, stopped := 0, 0, 0
	failed := []FailedUnit{}
	for _, u := range units {
		total++
		switch u.State {
		case StateRunning:
			running++
		case StateStopped:
			stopped++
		case StateFailed:
			failed = append(failed, FailedUnit{Name: u.Name, Description: u.Description, Result: "exit-code"})
		}
	}
	return map[string]any{"total": total, "running": running, "stopped": stopped, "timers": 0, "sockets": 0, "failed": failed}, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func handleGet(ctx context.Context, c *rpc.Call) (any, error) {
	var p nameParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := checkServiceName(p.Name); err != nil {
		return nil, err
	}
	m, err := connect()
	if err != nil {
		return nil, err
	}
	defer m.Disconnect()
	s, err := openService(m, p.Name, readRights)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	cfg, err := s.Config()
	if err != nil {
		return nil, scmError(err, p.Name, "read")
	}
	st, err := s.Query()
	if err != nil {
		return nil, scmError(err, p.Name, "read")
	}
	dependents, _ := s.ListDependentServices(svc.AnyActivity)
	return serviceDetails(unitFrom(p.Name, cfg, st), cfg, st, dependents), nil
}

func serviceDetails(u *Unit, cfg mgr.Config, st svc.Status, dependents []string) map[string]any {
	deps := map[string][]string{}
	for _, d := range depKeys {
		deps[d.key] = []string{}
	}
	deps["requires"] = nonNil(cfg.Dependencies)
	deps["requiredBy"] = nonNil(dependents)
	canStop := st.Accepts&svc.AcceptStop != 0
	result := ""
	if u.State == StateFailed {
		result = "exit-code"
	}
	startType := winStartTypeName(cfg.StartType, cfg.DelayedAutoStart)
	return map[string]any{
		"unit": u, "name": u.Name, "description": u.Description, "load": u.Load,
		"active": u.Active, "sub": u.Sub, "state": u.State, "enabled": u.Enabled,
		"memory": u.Memory, "cpuNs": u.CPUNs, "pid": u.PID, "since": u.Since, "purpose": u.Purpose,
		"type":          "win32",
		"path":          cfg.BinaryPathName,
		"dropIns":       []string{},
		"result":        result,
		"exitCode":      int(st.Win32ExitCode),
		"documentation": []string{},
		"canStart":      st.State == svc.Stopped && cfg.StartType != scmDisabled,
		"canStop":       canStop && st.State != svc.Stopped,
		"canReload":     false,
		"dependencies":  deps,
		"properties": map[string]string{
			"DisplayName": cfg.DisplayName, "StartType": startType, "ServiceStartName": cfg.ServiceStartName,
			"BinaryPathName": cfg.BinaryPathName, "LoadOrderGroup": cfg.LoadOrderGroup,
		},
	}
}

// waitState polls until the service reaches want or ctx ends.
func waitState(ctx context.Context, s *mgr.Service, want svc.State) error {
	for {
		st, err := s.Query()
		if err != nil {
			return err
		}
		if st.State == want {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func handleAction(ctx context.Context, c *rpc.Call) (any, error) {
	var p actionParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := checkServiceName(p.Name); err != nil {
		return nil, err
	}
	startType, isCfg := winActionStartType(p.Action)
	switch p.Action {
	case "start", "stop", "restart", "pause", "continue":
	case "reload":
		return nil, rpc.Errorf(rpc.Invalid, "Windows services have no reload. Use restart instead.")
	default:
		if !isCfg {
			return nil, rpc.Errorf(rpc.Invalid, "Unknown action %q. Use start, stop, restart, pause, continue, enable, disable, mask or unmask.", p.Action)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	m, err := connect()
	if err != nil {
		return nil, err
	}
	defer m.Disconnect()
	s, err := openService(m, p.Name, actionRights)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	fail := func(err error) (any, error) {
		if ctx.Err() != nil {
			return nil, rpc.Errorf(rpc.Unavailable, "Windows did not finish %s %s in time.", p.Action, p.Name)
		}
		return nil, scmError(err, p.Name, p.Action)
	}
	err = runAction(ctx, s, p.Action, startType)
	if err != nil {
		return fail(err)
	}
	return map[string]any{"ok": true}, nil
}

// runAction performs one validated action on s; the config actions set the
// start type.
func runAction(ctx context.Context, s *mgr.Service, action string, startType uint32) error {
	switch action {
	case "start":
		return s.Start()
	case "stop":
		return stopService(ctx, s)
	case "restart":
		if st, qerr := s.Query(); qerr == nil && st.State != svc.Stopped {
			if err := stopService(ctx, s); err != nil {
				return err
			}
		}
		return s.Start()
	case "pause":
		_, err := s.Control(svc.Pause)
		return err
	case "continue":
		_, err := s.Control(svc.Continue)
		return err
	}
	// enable, disable, mask, unmask: change the start type
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	cfg.StartType = startType
	cfg.DelayedAutoStart = false
	return s.UpdateConfig(cfg)
}

func stopService(ctx context.Context, s *mgr.Service) error {
	if _, err := s.Control(svc.Stop); err != nil {
		return err
	}
	return waitState(ctx, s, svc.Stopped)
}

func handleUnitFile(ctx context.Context, c *rpc.Call) (any, error) {
	return nil, rpc.Errorf(rpc.Unavailable, "Windows services have no unit file. The binary path and start type are shown in the details.")
}

func handleSaveOverride(ctx context.Context, c *rpc.Call) (any, error) {
	return nil, rpc.Errorf(rpc.Unavailable, "Overrides are a systemd feature and are not available on Windows.")
}

func handleLogs(ctx context.Context, c *rpc.Call) (any, error) {
	return nil, rpc.Errorf(rpc.Unavailable, "There is no service journal on Windows. Use the Logs section (Event Log) instead.")
}

func handleWatch(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p listParams
	if err := c.Bind(&p); err != nil {
		return err
	}
	typ, err := unitTypeParam(p.Type)
	if err != nil {
		return err
	}
	prev, _, err := watchSnapshot(typ)
	if err != nil {
		return err
	}
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.Input():
		case <-tick.C:
		}
		cur, ptrs, err := watchSnapshot(typ)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		units, removed := diffUnits(prev, cur, ptrs)
		prev = cur
		if len(units) == 0 && len(removed) == 0 {
			continue
		}
		if err := s.Send(map[string]any{"units": units, "removed": removed}); err != nil {
			return nil
		}
	}
}

// watchSnapshot lists the units of typ by name, as values to compare later
// and as pointers to send.
func watchSnapshot(typ string) (map[string]Unit, map[string]*Unit, error) {
	l, err := listUnits(typ)
	if err != nil {
		return nil, nil, err
	}
	v := make(map[string]Unit, len(l))
	ptr := make(map[string]*Unit, len(l))
	for _, u := range l {
		v[u.Name], ptr[u.Name] = *u, u
	}
	return v, ptr, nil
}

// diffUnits returns the new or changed units of cur and the names gone since
// prev, both sorted by name and never nil.
func diffUnits(prev, cur map[string]Unit, ptrs map[string]*Unit) ([]*Unit, []string) {
	units := []*Unit{}
	removed := []string{}
	for n, u := range cur {
		if o, ok := prev[n]; !ok || unitChanged(o, u) {
			units = append(units, ptrs[n])
		}
	}
	for n := range prev {
		if _, ok := cur[n]; !ok {
			removed = append(removed, n)
		}
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Name < units[j].Name })
	sort.Strings(removed)
	return units, removed
}

func unitChanged(o, u Unit) bool {
	return o.Active != u.Active || o.Sub != u.Sub || o.Enabled != u.Enabled || o.PID != u.PID || o.Description != u.Description
}
