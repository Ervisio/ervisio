//go:build !windows

package bridge

import "github.com/ervisio/ervisio/server/internal/rpc"

func rootDir() string { return "/" }

// rootHello: the bridge answered as uid 0 with admin methods.
func rootHello(h *rpc.Hello) bool { return h.UID == 0 }
