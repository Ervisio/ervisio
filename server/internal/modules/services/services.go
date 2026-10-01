// Package services implements the bridge methods of the Services section (services.*).
//
// Owned by the Services section agent: it fills in Register and adds any other
// files it needs inside this package. See docs/ARCHITECTURE.md and document
// the methods in docs/api/services.md.
package services

import "github.com/Fonlogen/LinuxAdmin/server/internal/rpc"

// Register adds the services.* methods to the registry.
func Register(r *rpc.Registry) {}
