// Package users implements the bridge methods of the Users section (users.*).
//
// Owned by the Users section agent: it fills in Register and adds any other
// files it needs inside this package. See docs/ARCHITECTURE.md and document
// the methods in docs/api/users.md.
package users

import "github.com/Fonlogen/LinuxAdmin/server/internal/rpc"

// Register adds the users.* methods to the registry.
func Register(r *rpc.Registry) {}
