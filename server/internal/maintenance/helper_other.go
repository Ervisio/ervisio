//go:build !linux

package maintenance

import (
	"context"
	"errors"
)

func PrepareHelper(*Plan, string) (*JobStatus, error) {
	return nil, errors.New("Factory reset requires the Linux safe-filesystem backend.")
}
func ReadStatus(string) (*JobStatus, error)      { return nil, errors.New("Factory reset is unavailable.") }
func FailPrepared(string, error)                 {}
func LaunchHelper(context.Context, string) error { return errors.New("Factory reset is unavailable.") }
func RunHelper([]string) int                     { return 2 }
