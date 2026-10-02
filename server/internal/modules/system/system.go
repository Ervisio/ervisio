// Package system implements the Overview section's bridge methods:
// host information, live metrics and power actions.
package system

import (
	"context"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// Register adds the system.* methods.
func Register(r *rpc.Registry) {
	shared := newSampler()
	r.Handle("system.host", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return readHost()
	})
	r.Handle("system.metrics", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return shared.metrics(ctx)
	})
	r.Stream("system.metricsStream", rpc.User, metricsStream)
	r.Handle("system.power", rpc.Admin, power)
}

// metricsStream sends a Metrics object every `interval` ms (default 2000,
// clamped to 250…600000). The first event is sent immediately.
func metricsStream(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p struct {
		Interval int `json:"interval"`
	}
	if err := c.Bind(&p); err != nil {
		return err
	}
	iv := time.Duration(p.Interval) * time.Millisecond
	switch {
	case p.Interval == 0:
		iv = 2 * time.Second
	case iv < 250*time.Millisecond:
		iv = 250 * time.Millisecond
	case iv > 10*time.Minute:
		iv = 10 * time.Minute
	}
	smp := newSampler()
	t := time.NewTicker(iv)
	defer t.Stop()
	for {
		m, err := smp.metrics(ctx)
		if err != nil {
			return err
		}
		if err := s.Send(m); err != nil {
			return err
		}
		t.Reset(iv) // the first sample may have taken a warm-up delay
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func power(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Action string `json:"action"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	switch p.Action {
	case "reboot", "poweroff":
	default:
		return nil, rpc.Errorf(rpc.Invalid, "action must be reboot or poweroff")
	}
	if err := sys.Run(ctx, "systemctl", p.Action); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}
