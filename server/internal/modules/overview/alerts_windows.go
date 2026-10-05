//go:build windows

package overview

import (
	"context"

	"github.com/ervisio/ervisio/server/internal/sys"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// failedUnits reports Automatic services that are stopped with a non-zero
// exit code, read-only from the service control manager.
func failedUnits(ctx context.Context) []Alert {
	h, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT|windows.SC_MANAGER_ENUMERATE_SERVICE)
	if err != nil {
		return nil
	}
	m := &mgr.Mgr{Handle: h}
	defer m.Disconnect()
	names, err := m.ListServices()
	if err != nil {
		return nil
	}
	var units []unitState
	for _, name := range names {
		if ctx.Err() != nil {
			return nil
		}
		s, err := openServiceRO(m, name)
		if err != nil {
			continue
		}
		cfg, cerr := s.Config()
		st, qerr := s.Query()
		s.Close()
		if cerr != nil || qerr != nil {
			continue
		}
		if cfg.StartType == mgr.StartAutomatic && st.State == svc.Stopped && serviceFailed(st.Win32ExitCode, st.ServiceSpecificExitCode) {
			units = append(units, unitState{Unit: name, Load: "loaded", Active: "failed", Sub: "failed", Description: cfg.DisplayName})
		}
	}
	return failedAlertsFromUnits(units, nil)
}

func openServiceRO(m *mgr.Mgr, name string) (*mgr.Service, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.OpenService(m.Handle, p, windows.SERVICE_QUERY_CONFIG|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return nil, err
	}
	return &mgr.Service{Name: name, Handle: h}, nil
}

// mountPoints lists the fixed drives.
func mountPoints() []string { return sys.FixedDrives() }

// swapKB approximates the page file as the commit limit above physical memory.
func swapKB() (total, free uint64, ok bool) {
	m, ok := sys.MemoryStatus()
	if !ok || m.PageTotal <= m.PhysTotal {
		return 0, 0, ok
	}
	total = m.PageTotal - m.PhysTotal
	if m.PageAvail > m.PhysAvail {
		free = m.PageAvail - m.PhysAvail
	}
	return total >> 10, min(free, total) >> 10, true
}
