//go:build !linux

package bridge

// Automatic low-memory tuning currently uses Linux's physical/cgroup counters.
// Other platforms retain the runtime default unless explicitly configured.
func memoryAvailableToBridge() uint64 { return 0 }
