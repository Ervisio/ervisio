// Package logs implements the bridge methods of the Logs section (logs.*).
//
// Owned by the Logs section agent: it fills in Register and adds any other
// files it needs inside this package. See docs/ARCHITECTURE.md and document
// the methods in docs/api/logs.md.
package logs

import "github.com/Fonlogen/LinuxAdmin/server/internal/rpc"

// Register adds the logs.* methods to the registry.
func Register(r *rpc.Registry) {}
