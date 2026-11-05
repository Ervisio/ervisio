//go:build linux

package overview

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func readTaskSample(ctx context.Context) (*taskSample, error) {
	return readLinuxTaskSample(ctx, "/proc")
}

func readLinuxTaskSample(ctx context.Context, root string) (*taskSample, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	total := memTotal()
	page := uint64(os.Getpagesize())
	sample := &taskSample{processes: make(map[int]taskCounter, len(entries))}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		data, err := os.ReadFile(filepath.Join(dir, "stat"))
		stat, ok := parseProcStat(data)
		if err != nil || !ok {
			// Preserve visible but restricted processes. Processes which exited
			// during this scan are omitted rather than returned as stale rows.
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				continue
			}
			sample.processes[pid] = taskCounter{row: TaskProcess{Process: Process{PID: pid, Name: entry.Name()}}}
			continue
		}
		row := TaskProcess{Process: Process{PID: pid, Name: stat.name, State: stat.state, Memory: stat.rss * page}, PPID: stat.ppid, Threads: stat.threads, CPUAvailable: true, MemoryAvailable: true}
		if total > 0 {
			row.MemPercent = round1(100 * float64(row.Memory) / float64(total))
		}
		if info, err := os.Stat(dir); err == nil {
			if uid, ok := fileOwner(info); ok {
				row.User = userName(uid)
			}
		}
		// The image path is useful without copying every process's potentially
		// large command line (which may contain secrets) into each snapshot.
		row.Command, _ = os.Readlink(filepath.Join(dir, "exe"))
		if row.Command == "" {
			row.Command = "[" + stat.name + "]"
		}
		row.Command = strings.TrimSpace(row.Command)
		sample.processes[pid] = taskCounter{row: row, cpu: stat.ticks * uint64(time.Second/100), started: stat.started}
	}
	sample.at = time.Now()
	return sample, nil
}
