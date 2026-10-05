//go:build windows

package bridge

import (
	"os"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func rootDir() string {
	return firstNonEmpty(os.Getenv("SYSTEMROOT"), os.Getenv("WINDIR"), `C:\Windows`)
}

// rootHello: Windows has no uid 0 (the bridge reports -1); the --admin
// flag is only accepted by an elevated token, so Hello.Admin is the proof.
func rootHello(h *rpc.Hello) bool { return h.Admin }
