// Package software implements the bridge methods of the Software section (software.*).
//
// Owned by the Software section agent: it fills in Register and adds any other
// files it needs inside this package. See docs/ARCHITECTURE.md and document
// the methods in docs/api/software.md.
package software

import "github.com/Fonlogen/LinuxAdmin/server/internal/rpc"

// Register adds the software.* methods to the registry.
func Register(r *rpc.Registry) {}
