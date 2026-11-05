//go:build windows

package overview

import (
	"context"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

func readTaskSample(ctx context.Context) (*taskSample, error) {
	processes := winSample()
	if processes == nil {
		return nil, rpc.Errorf(rpc.Unavailable, "process snapshot is unavailable")
	}
	var total uint64
	if memory, ok := sys.MemoryStatus(); ok {
		total = memory.PhysTotal
	}
	sample := &taskSample{processes: make(map[int]taskCounter, len(processes))}
	for _, process := range processes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		command, owner := winDescribe(process.pid, process.name)
		row := TaskProcess{Process: Process{PID: process.pid, Name: process.name, Command: command, User: owner, Memory: process.rss}, PPID: process.ppid, Threads: int(process.threads), CPUAvailable: process.cpuKnown, MemoryAvailable: process.memoryKnown}
		if total > 0 && row.MemoryAvailable {
			row.MemPercent = round1(100 * float64(row.Memory) / float64(total))
		}
		sample.processes[process.pid] = taskCounter{row: row, cpu: process.cpu100ns * 100, started: process.created}
	}
	sample.at = time.Now()
	return sample, nil
}

func readTaskGPUs(context.Context) []TaskGPU { return []TaskGPU{} }
