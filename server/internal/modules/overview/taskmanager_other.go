//go:build !linux && !windows

package overview

import (
	"context"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func readTaskSample(context.Context) (*taskSample, error) {
	return nil, rpc.Errorf(rpc.Unavailable, "process telemetry is unavailable on this platform")
}

func readTaskGPUs(context.Context) []TaskGPU { return []TaskGPU{} }
