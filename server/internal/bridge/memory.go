package bridge

import (
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

const lowMemoryBridgeBudget = 96 << 20

// ConfigureMemoryLimit applies a soft Go runtime memory budget. It is not an
// RSS cap and never kills a process or truncates a request to stay under it.
// Auto constrains bridges on Linux hosts/containers with at most 2 GiB RAM.
func ConfigureMemoryLimit(value string) error {
	if value == "auto" && os.Getenv("GOMEMLIMIT") != "" {
		// The Go runtime already applied an explicitly inherited operator value.
		return nil
	}
	limit, err := memoryLimit(value, memoryAvailableToBridge())
	if err != nil {
		return err
	}
	if limit >= 0 {
		debug.SetMemoryLimit(limit)
	}
	return nil
}

// A negative result leaves the runtime's existing policy untouched.
func memoryLimit(value string, available uint64) (int64, error) {
	if value == "auto" {
		if available > 0 && available <= 2<<30 {
			return lowMemoryBridgeBudget, nil
		}
		return -1, nil
	}
	if value == "off" {
		return math.MaxInt64, nil
	}
	units := []struct {
		suffix string
		bytes  uint64
	}{{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"B", 1}}
	for _, unit := range units {
		if number, ok := strings.CutSuffix(value, unit.suffix); ok {
			n, err := strconv.ParseUint(number, 10, 64)
			if err == nil && n <= math.MaxInt64/unit.bytes {
				return int64(n * unit.bytes), nil
			}
			break
		}
	}
	return 0, fmt.Errorf("invalid bridge memory limit %q: use auto, off, or an integer with B/KiB/MiB/GiB/TiB", value)
}
