//go:build !linux && !windows

package storage

import (
	"context"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

func inventory(context.Context) ([]Device, error) {
	return nil, rpc.Errorf(rpc.Unavailable, "Volume formatting is unavailable on this operating system.")
}
func sameDevice(d Device, name string) bool { return d.Device == name }
func formatVolume(context.Context, Device, formatRequest, func(any) error) error {
	return rpc.Errorf(rpc.Unavailable, "Volume formatting is unavailable on this operating system.")
}
