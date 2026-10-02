package overview

import (
	"bytes"
	"context"
	"os"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Process is one row of overview.processes.
type Process struct {
	PID        int     `json:"pid"`
	Name       string  `json:"name"`
	Command    string  `json:"command"`
	User       string  `json:"user"`
	State      string  `json:"state"`
	CPU        float64 `json:"cpu"`        // percent of one core, so 250 = 2.5 cores busy
	Memory     uint64  `json:"memory"`     // resident bytes
	MemPercent float64 `json:"memPercent"` // of total RAM
}

type procStat struct {
	pid   int
	name  string
	state string
	ticks uint64 // utime + stime
	rss   uint64 // pages
}

// parseProcStat parses /proc/<pid>/stat. The command name sits in parentheses
// and may itself contain spaces and parentheses, so split on the last ")".
func parseProcStat(b []byte) (procStat, bool) {
	s := string(bytes.TrimSpace(b))
	open := strings.IndexByte(s, '(')
	end := strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return procStat{}, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(s[:open]))
	if err != nil {
		return procStat{}, false
	}
	f := strings.Fields(s[end+1:])
	// f[0]=state ... f[11]=utime f[12]=stime ... f[21]=rss
	if len(f) < 22 {
		return procStat{}, false
	}
	ut, _ := strconv.ParseUint(f[11], 10, 64)
	st, _ := strconv.ParseUint(f[12], 10, 64)
	rss, _ := strconv.ParseInt(f[21], 10, 64)
	if rss < 0 {
		rss = 0
	}
	return procStat{pid: pid, name: s[open+1 : end], state: f[0], ticks: ut + st, rss: uint64(rss)}, true
}

func readStats() map[int]procStat {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	out := make(map[int]procStat, len(ents))
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		if ps, ok := parseProcStat(b); ok {
			out[pid] = ps
		}
	}
	return out
}

func memTotal() uint64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "MemTotal:") {
			f := strings.Fields(l)
			if len(f) >= 2 {
				n, _ := strconv.ParseUint(f[1], 10, 64)
				return n * 1024
			}
		}
	}
	return 0
}

// rankProcesses sorts by "cpu" or "mem" and keeps the first n.
func rankProcesses(list []Process, by string, n int) []Process {
	sort.SliceStable(list, func(i, j int) bool {
		if by == "mem" {
			if list[i].Memory != list[j].Memory {
				return list[i].Memory > list[j].Memory
			}
			return list[i].CPU > list[j].CPU
		}
		if list[i].CPU != list[j].CPU {
			return list[i].CPU > list[j].CPU
		}
		return list[i].Memory > list[j].Memory
	})
	if n < len(list) {
		list = list[:n]
	}
	return list
}

var userNames = map[uint32]string{}

func userName(uid uint32) string {
	if n, ok := userNames[uid]; ok {
		return n
	}
	s := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(s); err == nil {
		s = u.Username
	}
	userNames[uid] = s
	return s
}

func processes(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Sort  string `json:"sort"`
		Limit int    `json:"limit"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	switch p.Sort {
	case "", "cpu":
		p.Sort = "cpu"
	case "mem":
	default:
		return nil, rpc.Errorf(rpc.Invalid, "sort must be cpu or mem")
	}
	if p.Limit <= 0 {
		p.Limit = 5
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	const tick = 100.0 // USER_HZ is 100 on every Linux platform we run on
	t0 := time.Now()
	a := readStats()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(400 * time.Millisecond):
	}
	b := readStats()
	secs := time.Since(t0).Seconds()
	total := memTotal()
	page := uint64(os.Getpagesize())
	list := make([]Process, 0, len(b))
	for pid, cur := range b {
		prev, ok := a[pid]
		cpu := 0.0
		if ok && cur.ticks >= prev.ticks && secs > 0 {
			cpu = float64(cur.ticks-prev.ticks) / tick / secs * 100
		}
		pr := Process{PID: pid, Name: cur.name, State: cur.state, CPU: round1(cpu), Memory: cur.rss * page}
		if total > 0 {
			pr.MemPercent = round1(100 * float64(pr.Memory) / float64(total))
		}
		list = append(list, pr)
	}
	list = rankProcesses(list, p.Sort, p.Limit)
	for i := range list {
		list[i].Command, list[i].User = describePID(list[i].PID, list[i].Name)
	}
	return list, nil
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// describePID reads the owner and full command line of a process (best effort).
func describePID(pid int, name string) (cmdline, owner string) {
	dir := "/proc/" + strconv.Itoa(pid)
	if fi, err := os.Stat(dir); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			owner = userName(st.Uid)
		}
	}
	if b, err := os.ReadFile(dir + "/cmdline"); err == nil && len(b) > 0 {
		cmdline = strings.TrimSpace(strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\x00", " "))
	}
	if cmdline == "" {
		cmdline = "[" + name + "]"
	}
	if len(cmdline) > 300 {
		cmdline = cmdline[:300]
	}
	return
}
