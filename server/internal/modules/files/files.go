// Package files implements the bridge methods of the Files section (files.*).
//
// Owned by the Files section agent: it fills in Register and adds any other
// files it needs inside this package. See docs/ARCHITECTURE.md and document
// the methods in docs/api/files.md.
package files

import "github.com/Fonlogen/LinuxAdmin/server/internal/rpc"

// Register adds the files.* methods to the registry.
func Register(r *rpc.Registry) {}

// The daemon's HTTP download/upload endpoints rely on two stream methods that
// this package must provide: files.readStream and files.writeStream.
// Their wire shape is specified in docs/api/files-transfer.md.
