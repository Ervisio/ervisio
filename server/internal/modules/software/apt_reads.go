package software

import (
	"context"
	"time"
)

// APT may map the package databases for each read subprocess. Cache misses for
// different resources (updates, installed, suggestions) must not multiply that
// cost on a small server. Only the subprocess is gated, never its callers:
// Lookup/fill and application ownership queries can safely call one another.
type readGate chan struct{}

var aptReads = make(readGate, 1)

func (gate readGate) run(ctx context.Context, read func() (string, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return read()
}

func runAptRead(ctx context.Context, timeout time.Duration, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return aptReads.run(ctx, func() (string, error) { return run(ctx, timeout, env, name, args...) })
}
