package system

import (
	"context"
	"strings"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	c := parseProcStat(strings.NewReader("cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 50 0 50 350 50 0 0 0 0 0\nintr 1 2\n"))
	if len(c) != 2 || c[0].total != 1000 || c[0].idle != 800 {
		t.Fatalf("%+v", c)
	}
	if p := cpuPercent(cpuTimes{1000, 800}, cpuTimes{2000, 1300}); p != 50 {
		t.Fatalf("percent %v", p)
	}
}

func TestParseMeminfo(t *testing.T) {
	mi := parseMeminfo(strings.NewReader("MemTotal: 1000 kB\nMemFree: 100 kB\nMemAvailable: 600 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n"))
	mem, sw := memoryFrom(mi)
	if mem.Total != 1024000 || mem.Used != 400*1024 || mem.Percent != 40 || sw.Total != 0 {
		t.Fatalf("%+v %+v", mem, sw)
	}
}

func TestParseNetDev(t *testing.T) {
	n := parseNetDev(strings.NewReader(`Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  100 1 0 0 0 0 0 0  100 1 0 0 0 0 0 0
  eth0: 2000 10 0 0 0 0 0 0 3000 10 0 0 0 0 0 0
`))
	if n["eth0"].rx != 2000 || n["eth0"].tx != 3000 || len(n) != 2 {
		t.Fatalf("%+v", n)
	}
	out := netFrom(map[string]netCounters{"eth0": {1000, 1000}}, n, 2)
	if len(out) != 1 || out[0].RxRate != 500 || out[0].TxRate != 1000 {
		t.Fatalf("%+v", out)
	}
}

func TestParseMountinfo(t *testing.T) {
	m := parseMountinfo(strings.NewReader(`22 1 0:25 / / rw - btrfs /dev/nvme0n1p2 rw
23 22 0:25 /@home /home rw - btrfs /dev/nvme0n1p2 rw
24 22 259:1 / /boot rw - vfat /dev/nvme0n1p1 rw
25 22 0:5 / /dev rw - devtmpfs devtmpfs rw
26 22 0:30 / /run rw - tmpfs tmpfs rw
27 22 8:1 / /mnt/my\040disk rw shared:1 - ext4 /dev/sda1 rw
`))
	if len(m) != 3 || m[0].mount != "/" || m[1].mount != "/boot" || m[2].mount != "/mnt/my disk" {
		t.Fatalf("%+v", m)
	}
}

func TestParseCPUInfo(t *testing.T) {
	ci := parseCPUInfo(strings.NewReader("processor: 0\nmodel name: X\nphysical id: 0\ncore id: 0\n\nprocessor: 1\nmodel name: X\nphysical id: 0\ncore id: 0\n\nprocessor: 2\nphysical id: 0\ncore id: 1\n"))
	if ci.Threads != 3 || ci.Cores != 2 || ci.Model != "X" {
		t.Fatalf("%+v", ci)
	}
}

func TestLiveMetrics(t *testing.T) {
	m, err := newSampler().metrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if m.Memory.Total == 0 || len(m.CPU.Cores) == 0 || m.Interval <= 0 {
		t.Fatalf("%+v", m)
	}
	h, err := readHost()
	if err != nil || h.Kernel == "" || h.CPU.Threads == 0 {
		t.Fatalf("%+v %v", h, err)
	}
}

func TestWindowsKernel(t *testing.T) {
	if g := windowsKernel("22631", "3593"); g != "10.0.22631.3593" {
		t.Fatal(g)
	}
	if windowsKernel("", "1") != "" || windowsKernel("19045", "") != "10.0.19045" {
		t.Fatal("edge cases")
	}
}

func TestWindowsMemory(t *testing.T) {
	mem, sw := windowsMemory(16<<30, 4<<30, 20<<30, 6<<30)
	if mem.Used != 12<<30 || mem.Percent != 75 {
		t.Fatalf("%+v", mem)
	}
	if sw.Total != 4<<30 || sw.Free != 2<<30 || sw.Used != 2<<30 || sw.Percent != 50 {
		t.Fatalf("%+v", sw)
	}
	_, sw = windowsMemory(8<<30, 1<<30, 8<<30, 1<<30)
	if sw.Total != 0 {
		t.Fatalf("%+v", sw)
	}
}
