//go:build !windows

package overview

import (
	"context"
)

// windowsProcesses exists only so the shared code compiles; Linux never calls it.
func windowsProcesses(context.Context, string, int) ([]Process, error) { return nil, nil }
