//go:build !linux

package maintenance

import (
	"context"
	"errors"
)

func Preview(context.Context, Options) (*Plan, error) {
	return nil, errors.New("Factory reset requires the Linux safe-filesystem backend. No data was changed.")
}
func Apply(context.Context, *Plan, Service, func(string)) (*Result, error) {
	return nil, errors.New("Factory reset requires the Linux safe-filesystem backend. No data was changed.")
}
