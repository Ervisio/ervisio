//go:build unix

package system

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func takeSample() (*sample, error) {
	s := &sample{at: time.Now()}
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil, err
	}
	s.cpu = parseProcStat(f)
	f.Close()
	if f, err := os.Open("/proc/net/dev"); err == nil {
		s.net = parseNetDev(f)
		f.Close()
	}
	return s, nil
}

func readLoad() (l [3]float64) {
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(b))
		for i := 0; i < 3 && i < len(f); i++ {
			l[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	return l
}

func readMemory() (Memory, Swap, bool) {
	mi, err := readMeminfo()
	if err != nil {
		return Memory{}, Swap{}, false
	}
	mem, sw := memoryFrom(mi)
	return mem, sw, true
}

// ifaceFlags reports whether an interface is virtual and up.
func ifaceFlags(name string) (virtual, up bool) {
	if _, err := os.Stat(filepath.Join("/sys/class/net", name, "device")); err != nil {
		virtual = true
	}
	if b, err := os.ReadFile(filepath.Join("/sys/class/net", name, "operstate")); err == nil {
		up = strings.TrimSpace(string(b)) == "up"
	}
	return
}

// readDisks lists the real mounted filesystems.
func readDisks() []Disk {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return []Disk{}
	}
	entries := parseMountinfo(f)
	f.Close()
	out := []Disk{}
	for _, e := range entries {
		total, used, avail, err := diskUsage(e.mount)
		if err != nil || total == 0 {
			continue
		}
		d := Disk{Mount: e.mount, Device: e.source, FSType: e.fstype, Total: total, Used: used, Free: avail}
		d.Percent = pct(used, used+avail)
		out = append(out, d)
	}
	return out
}
