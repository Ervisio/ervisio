//go:build !windows

package bridge

import (
	"context"
	"os"
)

// adminViaToken is false: admin rights come from sudo (StartAdmin).
const adminViaToken = false

func startAdminToken(context.Context, *Spec, string) (*Proc, error) { panic("unreachable") }

// Privileged reports whether the process runs as root (--admin requires it).
func Privileged() bool { return os.Geteuid() == 0 }

// RunningAsServiceIdentity reports whether the process runs as the all-powerful
// identity --dev-insecure-noauth must never run as: root on unix.
func RunningAsServiceIdentity() bool { return os.Geteuid() == 0 }
