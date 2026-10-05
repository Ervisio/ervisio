//go:build !windows

package users

import "github.com/ervisio/ervisio/server/internal/rpc"

// registerPlatform lets a platform replace the Linux handlers; unix keeps them.
func registerPlatform(*rpc.Registry) bool { return false }
