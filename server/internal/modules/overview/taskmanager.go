package overview

import (
	"context"
	"runtime"
	"sort"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// TaskProcess reports unavailable measurements separately from an actual zero.
// CPU is percent of one core, matching overview.processes.
type TaskProcess struct {
	Process
	PPID            int  `json:"ppid"`
	Threads         int  `json:"threads"`
	CPUAvailable    bool `json:"cpuAvailable"`
	MemoryAvailable bool `json:"memoryAvailable"`
}

type TaskCapability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// TaskGPU contains only global device counters, never per-process estimates.
type TaskGPU struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Utilization *float64 `json:"utilization"`
	MemoryUsed  *uint64  `json:"memoryUsed"`
	MemoryTotal *uint64  `json:"memoryTotal"`
}

type TaskSnapshot struct {
	Source       string        `json:"source"`
	Platform     string        `json:"platform"`
	Time         int64         `json:"time"`
	Interval     float64       `json:"interval"`
	CPUCores     int           `json:"cpuCores"`
	Scope        string        `json:"scope"`
	Privileged   bool          `json:"privileged"`
	Processes    []TaskProcess `json:"processes"`
	GPUs         []TaskGPU     `json:"gpus"`
	Capabilities struct {
		PerProcessNetwork TaskCapability `json:"perProcessNetwork"`
		PerProcessGPU     TaskCapability `json:"perProcessGPU"`
		GPU               TaskCapability `json:"gpu"`
	} `json:"capabilities"`
}

type taskCounter struct {
	row     TaskProcess
	cpu     uint64 // platform-independent nanoseconds
	started uint64 // detects PID reuse between samples
}
type taskSample struct {
	at        time.Time
	processes map[int]taskCounter
}
type taskSampler struct {
	gate     chan struct{}
	prev     *taskSample
	cached   *TaskSnapshot
	cachedAt time.Time
	read     func(context.Context) (*taskSample, error)
	gpus     func(context.Context) []TaskGPU
}

func newTaskSampler() *taskSampler {
	return &taskSampler{gate: make(chan struct{}, 1), read: readTaskSample, gpus: readTaskGPUs}
}

func registerTaskManager(r *rpc.Registry) {
	sampler := newTaskSampler()
	r.Handle("overview.taskManager", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return sampler.snapshot(ctx, c.Admin)
	})
}

// Calls in one bridge share sampling and a short cache. Concurrent windows do
// not create duplicate process scans or overlapping GPU command subprocesses.
func (s *taskSampler) snapshot(ctx context.Context, privileged bool) (*TaskSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.cached != nil && time.Since(s.cachedAt) < time.Second {
		return s.cached, nil
	}
	prev := s.prev
	if prev == nil || time.Since(prev.at) > 2*time.Minute {
		var err error
		prev, err = s.read(ctx)
		if err != nil {
			return nil, err
		}
		select {
		case <-time.After(400 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	cur, err := s.read(ctx)
	if err != nil {
		return nil, err
	}
	s.prev = cur
	result := &TaskSnapshot{Source: "host", Platform: runtime.GOOS, Time: cur.at.UnixMilli(), Interval: cur.at.Sub(prev.at).Seconds(), CPUCores: runtime.NumCPU(), Scope: "visible", Privileged: privileged, Processes: buildTaskProcesses(prev, cur), GPUs: s.gpus(ctx)}
	unsupported := TaskCapability{Reason: "not_available"}
	result.Capabilities.PerProcessNetwork = unsupported
	result.Capabilities.PerProcessGPU = unsupported
	result.Capabilities.GPU = unsupported
	if len(result.GPUs) > 0 {
		result.Capabilities.GPU = TaskCapability{Supported: true}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.cached = result
	s.cachedAt = time.Now()
	return result, nil
}

func buildTaskProcesses(prev, cur *taskSample) []TaskProcess {
	elapsed := cur.at.Sub(prev.at)
	list := make([]TaskProcess, 0, len(cur.processes))
	for pid, counter := range cur.processes {
		row := counter.row
		p, known := prev.processes[pid]
		row.CPUAvailable = known && p.row.CPUAvailable && row.CPUAvailable && p.started == counter.started && counter.cpu >= p.cpu && elapsed > 0
		row.CPU = 0
		if row.CPUAvailable {
			row.CPU = round1(float64(counter.cpu-p.cpu) / float64(elapsed) * 100)
		}
		list = append(list, row)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].CPU != list[j].CPU {
			return list[i].CPU > list[j].CPU
		}
		if list[i].Memory != list[j].Memory {
			return list[i].Memory > list[j].Memory
		}
		return list[i].PID < list[j].PID
	})
	return list
}
