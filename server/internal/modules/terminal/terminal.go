// Package terminal implements the bridge methods of the Terminal section (terminal.*).
//
// Owned by the Terminal section agent: it fills in Register and adds any other
// files it needs inside this package. See docs/ARCHITECTURE.md and document
// the methods in docs/api/terminal.md.
package terminal

import "github.com/Fonlogen/LinuxAdmin/server/internal/rpc"

// Register adds the terminal.* methods to the registry.
func Register(r *rpc.Registry) {}
