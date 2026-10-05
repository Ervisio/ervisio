//go:build !windows

package main

import (
	"context"
	"os/signal"
	"syscall"
)

// exeSuffix is appended to the names of the executables next to ervisiod.
const exeSuffix = ""

// runAsService reports whether the process was started as an OS service
// and ran run in that role. Only Windows has such a mode here (systemd
// runs ervisiod as a plain foreground process).
func runAsService(run func(context.Context)) bool { return false }

// consoleContext is cancelled by SIGINT or SIGTERM.
func consoleContext() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	signal.Ignore(syscall.SIGPIPE)
	return ctx, stop
}
