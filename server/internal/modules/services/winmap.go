package services

// Platform-neutral mapping of Windows Service Control Manager values onto the
// shapes the web UI already knows from systemd. It uses the raw numeric SCM
// constants so it builds (and is tested) on every platform.

import (
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// SERVICE_* current states (winsvc.h).
const (
	scmStopped         = 1
	scmStartPending    = 2
	scmStopPending     = 3
	scmRunning         = 4
	scmContinuePending = 5
	scmPausePending    = 6
	scmPaused          = 7
)

// SERVICE_*_START start types.
const (
	scmBootStart   = 0
	scmSystemStart = 1
	scmAutoStart   = 2
	scmDemandStart = 3
	scmDisabled    = 4
)

// Win32 error codes the SCM returns.
const (
	errAccessDenied        = 5
	errInvalidName         = 123
	errServiceDoesNotExist = 1060
	errServiceNeverStarted = 1077
	errServiceAlreadyRun   = 1056
	errServiceNotActive    = 1062
	errServiceDisabled     = 1058
	errCannotAcceptCtrl    = 1061
	errServiceMarkedDelete = 1072
	errDependentRunning    = 1051
	errServiceRequestTmout = 1053
)

// winState maps a SCM current state (and the exit code of a stopped service)
// to the active/sub/state triple of a Unit.
func winState(state, win32Exit uint32) (active, sub, simple string) {
	switch state {
	case scmRunning:
		return "active", "running", StateRunning
	case scmStartPending:
		return "activating", "start-pending", StateRunning
	case scmContinuePending:
		return "activating", "continue-pending", StateRunning
	case scmPaused:
		return "active", "paused", StateRunning
	case scmPausePending:
		return "active", "pause-pending", StateRunning
	case scmStopPending:
		return "deactivating", "stop-pending", StateStopped
	case scmStopped:
		if win32Exit != 0 && win32Exit != errServiceNeverStarted {
			return "failed", "failed", StateFailed
		}
		return "inactive", "dead", StateStopped
	}
	return "inactive", "dead", StateStopped
}

// winEnabled maps a start type onto the systemd "enabled" notion:
// automatic (also delayed) = enabled, manual = disabled (can be enabled),
// disabled = masked, boot/system (drivers) = static.
func winEnabled(startType uint32, delayed bool) string {
	switch startType {
	case scmAutoStart:
		return "enabled"
	case scmDemandStart:
		return "disabled"
	case scmDisabled:
		return "masked"
	case scmBootStart, scmSystemStart:
		return "static"
	}
	return ""
}

// winStartTypeName is the human name of a start type (auto, delayed, manual, disabled, boot, system).
func winStartTypeName(startType uint32, delayed bool) string {
	switch startType {
	case scmAutoStart:
		if delayed {
			return "delayed"
		}
		return "auto"
	case scmDemandStart:
		return "manual"
	case scmDisabled:
		return "disabled"
	case scmBootStart:
		return "boot"
	case scmSystemStart:
		return "system"
	}
	return ""
}

// winActionStartType returns the start type an enable-style action sets.
func winActionStartType(action string) (uint32, bool) {
	switch action {
	case "enable":
		return scmAutoStart, true
	case "disable", "unmask":
		return scmDemandStart, true
	case "mask":
		return scmDisabled, true
	}
	return 0, false
}

// validServiceName reports whether name can be a Windows service name: 1..256
// characters, no slash or backslash, no control characters.
func validServiceName(name string) bool {
	if name == "" || len(name) > 256 || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func checkServiceName(name string) error {
	if !validServiceName(name) {
		return rpc.Errorf(rpc.Invalid, "%q is not a valid service name.", name)
	}
	return nil
}

// winError maps a Win32 error number from the SCM to an rpc error.
func winError(errno uint32, name, what string) error {
	switch errno {
	case errAccessDenied:
		return rpc.Errorf(rpc.NeedsAdmin, "Changing this service needs administrator rights.")
	case errServiceDoesNotExist, errInvalidName:
		return rpc.Errorf(rpc.NotFound, "Service %s was not found.", name)
	case errServiceAlreadyRun:
		return rpc.Errorf(rpc.Conflict, "Service %s is already running.", name)
	case errServiceNotActive:
		return rpc.Errorf(rpc.Conflict, "Service %s is not running.", name)
	case errServiceDisabled:
		return rpc.Errorf(rpc.Conflict, "Service %s is disabled. Enable it first.", name)
	case errCannotAcceptCtrl:
		return rpc.Errorf(rpc.Conflict, "Service %s cannot accept this command in its current state.", name)
	case errServiceMarkedDelete:
		return rpc.Errorf(rpc.Conflict, "Service %s is marked for deletion.", name)
	case errDependentRunning:
		return rpc.Errorf(rpc.Conflict, "Other running services depend on %s.", name)
	case errServiceRequestTmout:
		return rpc.Errorf(rpc.Unavailable, "Service %s did not respond in time.", name)
	}
	return rpc.Errorf(rpc.Conflict, "Could not %s %s (Windows error %d).", what, name, errno)
}
