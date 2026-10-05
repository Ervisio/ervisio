//go:build windows

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// exeSuffix is appended to the names of the executables next to ervisiod.
const exeSuffix = ".exe"

// serviceName is the name the installer registers with the service manager
// (packaging/windows/install.ps1).
const serviceName = brand.Name

// stopTimeout bounds how long a Stop request waits for the server to wind
// down before the service reports stopped anyway.
const stopTimeout = 25 * time.Second

// consoleContext is cancelled by Ctrl+C (and Ctrl+Break / console close).
func consoleContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// runAsService runs run under the Windows service manager when the process
// was started by it, and reports whether it did. Started from a console
// (including --dev) it returns false and main continues normally.
func runAsService(run func(context.Context)) bool {
	isSvc, err := svc.IsWindowsService()
	if err != nil || !isSvc {
		return false
	}
	// A service has no console: keep a log file under %ProgramData%.
	logDir := filepath.Join(brand.DataRoot, "logs")
	if err := os.MkdirAll(logDir, 0o700); err == nil {
		if f, err := os.OpenFile(filepath.Join(logDir, brand.DaemonBinary+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			log.SetOutput(f)
			os.Stderr = f
		}
	}
	if err := svc.Run(serviceName, &handler{run: run}); err != nil {
		log.Printf("service %s: %v", serviceName, err)
		os.Exit(1)
	}
	return true
}

type handler struct{ run func(context.Context) }

// Execute implements svc.Handler: it starts the daemon and cancels its
// context on Stop or Shutdown.
func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.run(ctx)
	}()
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case <-done:
			// The daemon ended by itself (a fatal error is logged by run).
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(stopTimeout):
					log.Printf("stop: the server did not finish within %s", stopTimeout)
				}
				return false, 0
			}
		}
	}
}
