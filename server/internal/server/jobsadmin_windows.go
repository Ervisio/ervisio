//go:build windows

package server

import (
	"os"
	"path/filepath"

	"github.com/ervisio/ervisio/server/internal/account"
)

// adminJobAccount is the synthetic account of the bridge that runs
// approved admin job steps: the daemon's own identity, LocalSystem
// (S-1-5-18), started without a token switch. SYSTEM's token is elevated,
// so the bridge's --admin check passes.
func adminJobAccount() (*account.Account, error) {
	sysRoot := os.Getenv("SYSTEMROOT")
	if sysRoot == "" {
		sysRoot = `C:\Windows`
	}
	return &account.Account{
		Name:       "SYSTEM",
		UID:        18,
		GID:        18,
		Groups:     []uint32{544},
		GroupNames: []string{"Administrators"},
		GroupSIDs:  []string{"S-1-5-32-544"},
		SID:        "S-1-5-18",
		Home:       filepath.Join(sysRoot, "System32", "config", "systemprofile"),
	}, nil
}
