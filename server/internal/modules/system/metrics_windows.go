//go:build windows

package system

import (
	"runtime"
	"sort"
	"time"

	"github.com/ervisio/ervisio/server/internal/sys"
)

func takeSample() (*sample, error) {
	s := &sample{at: time.Now()}
	idle, total, ok := sys.SystemTimes()
	if !ok {
		return nil, errNoCPU
	}
	s.cpu = []cpuTimes{{total: total, idle: idle}}
	if ci, ct, ok := sys.CoreTimes(runtime.NumCPU()); ok {
		for i := range ci {
			s.cpu = append(s.cpu, cpuTimes{total: ct[i], idle: ci[i]})
		}
	}
	if ifs, ok := sys.IfCounters(); ok {
		s.net = map[string]netCounters{}
		for _, i := range ifs {
			s.net[i.Name] = netCounters{rx: i.Rx, tx: i.Tx}
		}
	}
	return s, nil
}

// readLoad: Windows has no load average; it stays zero.
func readLoad() (l [3]float64) { return l }

func readMemory() (Memory, Swap, bool) {
	ms, ok := sys.MemoryStatus()
	if !ok {
		return Memory{}, Swap{}, false
	}
	mem, sw := windowsMemory(ms.PhysTotal, ms.PhysAvail, ms.PageTotal, ms.PageAvail)
	return mem, sw, true
}

// ifaceFlags reports whether an interface is virtual and up.
func ifaceFlags(name string) (virtual, up bool) {
	ifs, ok := sys.IfCounters()
	if !ok {
		return true, false
	}
	for _, i := range ifs {
		if i.Name == name {
			return !i.Hardware, i.Up
		}
	}
	return true, false
}

func readDisks() []Disk {
	out := []Disk{}
	for _, root := range sys.FixedDrives() {
		total, _, avail, err := sys.DiskSpace(root)
		if err != nil || total == 0 {
			continue
		}
		used := total - min(total, avail)
		d := Disk{Mount: root, Device: root, FSType: sys.VolumeFS(root), Total: total, Used: used, Free: avail}
		d.Percent = pct(used, total)
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mount < out[j].Mount })
	return out
}
