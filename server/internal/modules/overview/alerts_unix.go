//go:build unix

package overview

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/ervisio/ervisio/server/internal/sys"
)

func failedUnits(ctx context.Context) []Alert {
	out, err := sys.Output(ctx, "systemctl", "list-units", "--state=failed", "--output=json", "--no-pager", "--all")
	if err != nil {
		return nil
	}
	return failedAlerts(out, func(unit string) string { return failedSince(ctx, unit) })
}

// mountPoints lists the mount points from /proc/mounts.
func mountPoints() []string {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	for _, m := range parseMounts(f) {
		out = append(out, m.point)
	}
	return out
}

// swapKB returns the swap size and free swap, in kB.
func swapKB() (total, free uint64, ok bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, found := strings.Cut(sc.Text(), ":")
		if !found {
			continue
		}
		fs := strings.Fields(v)
		if len(fs) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(fs[0], 10, 64)
		switch k {
		case "SwapTotal":
			total = n
		case "SwapFree":
			free = n
		}
	}
	return total, free, true
}
