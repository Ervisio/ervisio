// Package plugins implements the bridge methods of the Plugins section (plugins.*).
//
// Owned by the Plugins section agent: it fills in Register and adds any other
// files it needs inside this package. See docs/ARCHITECTURE.md and document
// the methods in docs/api/plugins.md.
package plugins

import "github.com/Fonlogen/LinuxAdmin/server/internal/rpc"

// Register adds the plugins.* methods to the registry.
func Register(r *rpc.Registry) {}
