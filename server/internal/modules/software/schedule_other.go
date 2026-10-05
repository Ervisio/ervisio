//go:build !windows

package software

import (
	"context"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func currentScheduleOS() string { return "" }

func removeScheduleOS(context.Context) error { return nil }

func setScheduleOS(context.Context, []Plan, string) (string, error) {
	return "", rpc.Errorf(rpc.Unavailable, "Scheduled updates are not available on this system.")
}

func osUpdatesCount(context.Context) int { return -1 }
