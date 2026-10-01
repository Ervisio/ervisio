package system

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// CPU usage, in percent (0–100).
type CPU struct {
	Percent float64   `json:"percent"`
	Cores   []float64 `json:"cores"`
}

// Memory figures in bytes. Used = Total - Available.
type Memory struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	Free      uint64  `json:"free"`
	Buffers   uint64  `json:"buffers"`
	Cached    uint64  `json:"cached"`
	Percent   float64 `json:"percent"`
}

// Swap figures in bytes.
type Swap struct {
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"`
	Percent float64 `json:"percent"`
}

// Disk is one mounted real filesystem (bytes).
type Disk struct {
	Mount   string  `json:"mount"`
	Device  string  `json:"device"`
	FSType  string  `json:"fstype"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"` // available to unprivileged users
	Percent float64 `json:"percent"`
}

// Net is one network interface; rates in bytes per second.
type Net struct {
	Iface   string  `json:"iface"`
	RxBytes uint64  `json:"rxBytes"`
	TxBytes uint64  `json:"txBytes"`
	RxRate  float64 `json:"rxRate"`
	TxRate  float64 `json:"txRate"`
	Virtual bool    `json:"virtual"`
	Up      bool    `json:"up"`
}

// Metrics is the result of system.metrics and each system.metricsStream event.
type Metrics struct {
	Time     int64      `json:"time"`     // unix milliseconds
	Interval float64    `json:"interval"` // seconds covered by the rates
	CPU      CPU        `json:"cpu"`
	Load     [3]float64 `json:"load"`
	Memory   Memory     `json:"memory"`
	Swap     Swap       `json:"swap"`
	Disks    []Disk     `json:"disks"`
	Net      []Net      `json:"net"`
}

type cpuTimes struct{ total, idle uint64 }

type netCounters struct{ rx, tx uint64 }

type sample struct {
	at  time.Time
	cpu []cpuTimes // [0] = aggregate, [1:] = per core
	net map[string]netCounters
}

// sampler keeps the previous sample to compute rates.
type sampler struct {
	mu   sync.Mutex
	prev *sample
}

func newSampler() *sampler { return &sampler{} }

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

func (sm *sampler) metrics(ctx context.Context) (*Metrics, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	cur, err := takeSample()
	if err != nil {
		return nil, err
	}
	prev := sm.prev
	if prev == nil || cur.at.Sub(prev.at) > 30*time.Second || len(prev.cpu) != len(cur.cpu) {
		// No usable baseline: take a short second sample.
		prev = cur
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if cur, err = takeSample(); err != nil {
			return nil, err
		}
	}
	sm.prev = cur

	dt := cur.at.Sub(prev.at).Seconds()
	m := &Metrics{Time: cur.at.UnixMilli(), Interval: round(dt, 3), Disks: []Disk{}, Net: []Net{}}
	if len(cur.cpu) > 0 {
		m.CPU.Percent = cpuPercent(prev.cpu[0], cur.cpu[0])
		m.CPU.Cores = make([]float64, 0, len(cur.cpu)-1)
		for i := 1; i < len(cur.cpu); i++ {
			m.CPU.Cores = append(m.CPU.Cores, cpuPercent(prev.cpu[i], cur.cpu[i]))
		}
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(b))
		for i := 0; i < 3 && i < len(f); i++ {
			m.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	if mi, err := readMeminfo(); err == nil {
		m.Memory, m.Swap = memoryFrom(mi)
	}
	m.Disks = readDisks()
	m.Net = netFrom(prev.net, cur.net, dt)
	return m, nil
}

func cpuPercent(a, b cpuTimes) float64 {
	dt := float64(b.total) - float64(a.total)
	if dt <= 0 {
		return 0
	}
	busy := dt - (float64(b.idle) - float64(a.idle))
	return round(clamp(busy/dt*100, 0, 100), 1)
}

func clamp(v, lo, hi float64) float64 { return max(lo, min(hi, v)) }

func round(v float64, places int) float64 {
	p := 1.0
	for i := 0; i < places; i++ {
		p *= 10
	}
	return float64(int64(v*p+0.5)) / p
}

func pct(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return round(float64(part)/float64(total)*100, 1)
}

// parseProcStat returns aggregate + per-core CPU times from /proc/stat.
func parseProcStat(r io.Reader) []cpuTimes {
	var out []cpuTimes
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		var t cpuTimes
		// user nice system idle iowait irq softirq steal (guest is included in user)
		for i, v := range f[1:] {
			if i >= 8 {
				break
			}
			n, _ := strconv.ParseUint(v, 10, 64)
			t.total += n
			if i == 3 || i == 4 {
				t.idle += n
			}
		}
		out = append(out, t)
	}
	return out
}

func readMeminfo() (map[string]uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseMeminfo(f), nil
}

// parseMeminfo returns /proc/meminfo values in bytes.
func parseMeminfo(r io.Reader) map[string]uint64 {
	m := map[string]uint64{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 1 && f[1] == "kB" {
			n *= 1024
		}
		m[k] = n
	}
	return m
}

func memoryFrom(mi map[string]uint64) (Memory, Swap) {
	mem := Memory{Total: mi["MemTotal"], Free: mi["MemFree"], Buffers: mi["Buffers"],
		Cached: mi["Cached"] + mi["SReclaimable"]}
	avail, ok := mi["MemAvailable"]
	if !ok {
		avail = mem.Free + mem.Buffers + mem.Cached
	}
	mem.Available = min(avail, mem.Total)
	mem.Used = mem.Total - mem.Available
	mem.Percent = pct(mem.Used, mem.Total)
	sw := Swap{Total: mi["SwapTotal"], Free: min(mi["SwapFree"], mi["SwapTotal"])}
	sw.Used = sw.Total - sw.Free
	sw.Percent = pct(sw.Used, sw.Total)
	return mem, sw
}

// parseNetDev parses /proc/net/dev.
func parseNetDev(r io.Reader) map[string]netCounters {
	out := map[string]netCounters{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		out[strings.TrimSpace(name)] = netCounters{rx: rx, tx: tx}
	}
	return out
}

func netFrom(prev, cur map[string]netCounters, dt float64) []Net {
	out := []Net{}
	for name, c := range cur {
		if name == "lo" {
			continue
		}
		n := Net{Iface: name, RxBytes: c.rx, TxBytes: c.tx}
		if p, ok := prev[name]; ok && dt > 0 && c.rx >= p.rx && c.tx >= p.tx {
			n.RxRate = round(float64(c.rx-p.rx)/dt, 1)
			n.TxRate = round(float64(c.tx-p.tx)/dt, 1)
		}
		if _, err := os.Stat(filepath.Join("/sys/class/net", name, "device")); err != nil {
			n.Virtual = true
		}
		if b, err := os.ReadFile(filepath.Join("/sys/class/net", name, "operstate")); err == nil {
			n.Up = strings.TrimSpace(string(b)) == "up"
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Virtual != out[j].Virtual {
			return !out[i].Virtual
		}
		return out[i].Iface < out[j].Iface
	})
	return out
}

// realFS lists filesystem types that hold user data.
var realFS = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true, "f2fs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "fuseblk": true, "jfs": true, "reiserfs": true,
	"bcachefs": true, "nfs": true, "nfs4": true, "cifs": true, "smb3": true, "fuse.sshfs": true,
}

type mountEntry struct {
	devID, mount, fstype, source string
}

// parseMountinfo parses /proc/self/mountinfo, keeping real filesystems and
// one mount per device (the shortest mount point).
func parseMountinfo(r io.Reader) []mountEntry {
	byDev := map[string]mountEntry{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		pre, post, ok := strings.Cut(sc.Text(), " - ")
		if !ok {
			continue
		}
		a, b := strings.Fields(pre), strings.Fields(post)
		if len(a) < 5 || len(b) < 2 {
			continue
		}
		e := mountEntry{devID: a[2], mount: unescapeMount(a[4]), fstype: b[0], source: unescapeMount(b[1])}
		if !realFS[e.fstype] {
			continue
		}
		if strings.HasPrefix(e.mount, "/proc") || strings.HasPrefix(e.mount, "/sys") || strings.HasPrefix(e.mount, "/dev/") {
			continue
		}
		if old, ok := byDev[e.devID]; !ok || len(e.mount) < len(old.mount) {
			byDev[e.devID] = e
		}
	}
	out := make([]mountEntry, 0, len(byDev))
	for _, e := range byDev {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mount < out[j].mount })
	return out
}

// unescapeMount decodes the octal escapes (\040 etc.) used in mountinfo.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				sb.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

func readDisks() []Disk {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return []Disk{}
	}
	entries := parseMountinfo(f)
	f.Close()
	out := []Disk{}
	for _, e := range entries {
		var st syscall.Statfs_t
		if err := statfsTimeout(e.mount, &st); err != nil {
			continue
		}
		bs := uint64(st.Bsize)
		total := st.Blocks * bs
		if total == 0 {
			continue
		}
		used := (st.Blocks - st.Bfree) * bs
		avail := st.Bavail * bs
		d := Disk{Mount: e.mount, Device: e.source, FSType: e.fstype, Total: total, Used: used, Free: avail}
		d.Percent = pct(used, used+avail)
		out = append(out, d)
	}
	return out
}

// statfsTimeout guards against hung network filesystems.
func statfsTimeout(path string, st *syscall.Statfs_t) error {
	done := make(chan error, 1)
	var local syscall.Statfs_t
	go func() { done <- syscall.Statfs(path, &local) }()
	select {
	case err := <-done:
		*st = local
		return err
	case <-time.After(time.Second):
		return fmt.Errorf("statfs %s: timeout", path)
	}
}
