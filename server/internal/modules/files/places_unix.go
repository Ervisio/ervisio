//go:build unix

package files

import (
	"os"
	"sort"
	"syscall"
)

func readDisks() []disk {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return []disk{}
	}
	out := []disk{}
	for _, m := range pickDisks(parseMountinfo(string(data))) {
		var st syscall.Statfs_t
		if err := syscall.Statfs(m.Mount, &st); err != nil || st.Blocks == 0 {
			continue
		}
		bs := uint64(st.Bsize)
		total := st.Blocks * bs
		out = append(out, disk{Mount: m.Mount, Device: m.Source, FS: m.FS, Total: total,
			Used: total - st.Bfree*bs, Avail: st.Bavail * bs})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Mount < out[j].Mount })
	return out
}
