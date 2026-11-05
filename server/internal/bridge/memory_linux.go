//go:build linux

package bridge

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func memoryAvailableToBridge() uint64 {
	var total uint64
	if file, err := os.Open("/proc/meminfo"); err == nil {
		defer func() { _ = file.Close() }()
		scan := bufio.NewScanner(file)
		for scan.Scan() {
			fields := strings.Fields(scan.Text())
			if len(fields) >= 3 && fields[0] == "MemTotal:" {
				kib, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil && kib <= ^uint64(0)/1024 {
					total = kib * 1024
				}
				break
			}
		}
	}
	// These root-controller files cover common container layouts. Never use
	// MemAvailable: a temporary spike must not change the chosen startup policy.
	for _, path := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		if data, err := os.ReadFile(path); err == nil {
			if limit, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil && limit > 0 && (total == 0 || limit < total) {
				total = limit
			}
		}
	}
	return total
}
