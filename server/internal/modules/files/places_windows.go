//go:build windows

package files

// readDisks lists no disks on Windows yet.
func readDisks() []disk { return []disk{} }
