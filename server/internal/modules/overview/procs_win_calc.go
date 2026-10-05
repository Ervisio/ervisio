package overview

import (
	"sync"
	"time"
)

// winProc is one process as sampled from the Windows toolhelp snapshot.
// It is platform neutral so the CPU math can be tested anywhere.
type winProc struct {
	pid, ppid int
	name      string
	threads   uint32
	cpu100ns  uint64 // kernel + user time, 100ns units
	rss       uint64 // working set bytes
}

// cpuPercent converts a delta of 100ns CPU time over elapsed wall time into
// percent of one core (250 = 2.5 cores busy), like the Linux path.
func cpuPercent(prev, cur uint64, elapsed time.Duration) float64 {
	if cur < prev || elapsed <= 0 {
		return 0
	}
	return float64(cur-prev) / 1e7 / elapsed.Seconds() * 100
}

// winProcesses builds result rows from two samples; processes missing from
// prev (new, or not openable) report 0% CPU.
func winProcesses(prev map[int]uint64, elapsed time.Duration, cur []winProc, total uint64) []Process {
	list := make([]Process, 0, len(cur))
	for _, c := range cur {
		cpu := 0.0
		if p, ok := prev[c.pid]; ok {
			cpu = cpuPercent(p, c.cpu100ns, elapsed)
		}
		pr := Process{PID: c.pid, Name: c.name, Memory: c.rss, CPU: round1(cpu)}
		if total > 0 {
			pr.MemPercent = round1(100 * float64(c.rss) / float64(total))
		}
		list = append(list, pr)
	}
	return list
}

func cpuMap(l []winProc) map[int]uint64 {
	m := make(map[int]uint64, len(l))
	for _, p := range l {
		m[p.pid] = p.cpu100ns
	}
	return m
}

// winPrev is the previous sample kept between calls.
var winPrev struct {
	sync.Mutex
	at  time.Time
	cpu map[int]uint64
}

// usablePrev reports whether a kept sample is recent enough to take deltas from.
func usablePrev(at, now time.Time) bool {
	age := now.Sub(at)
	return !at.IsZero() && age >= 200*time.Millisecond && age <= 2*time.Minute
}
