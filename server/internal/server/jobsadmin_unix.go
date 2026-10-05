//go:build !windows

package server

import (
	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// adminJobAccount is the account of the bridge that runs approved admin
// job steps: root (the daemon is root).
func adminJobAccount() (*account.Account, error) {
	ra, err := account.Lookup("root")
	if err != nil {
		return nil, rpc.Errorf(rpc.Unavailable, "Cannot look up root: %v", err)
	}
	return ra, nil
}
