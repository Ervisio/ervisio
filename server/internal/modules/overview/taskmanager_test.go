package overview

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskProcessDeltasAndUnavailable(t *testing.T) {
	at := time.Now()
	entry := func(pid int, cpu uint64, started uint64, available bool) taskCounter {
		return taskCounter{row: TaskProcess{Process: Process{PID: pid, Memory: 2048}, CPUAvailable: available, MemoryAvailable: available}, cpu: cpu, started: started}
	}
	previous := &taskSample{at: at, processes: map[int]taskCounter{1: entry(1, 100, 1, true), 2: entry(2, 0, 1, true), 3: entry(3, 100, 1, true), 4: entry(4, 100, 1, true)}}
	current := &taskSample{at: at.Add(time.Second), processes: map[int]taskCounter{1: entry(1, 2_500_000_100, 1, true), 2: entry(2, 500_000_000, 2, true), 3: entry(3, 200, 1, false), 4: entry(4, 50, 1, true), 5: entry(5, 0, 1, true)}}
	rows := buildTaskProcesses(previous, current)
	if len(rows) != 5 || rows[0].PID != 1 || rows[0].CPU != 250 || !rows[0].CPUAvailable {
		t.Fatalf("actual interval CPU/all rows: %+v", rows)
	}
	for _, row := range rows[1:] {
		if row.CPUAvailable || row.CPU != 0 {
			t.Fatalf("new/reused/restricted/reset process must have unavailable CPU: %+v", row)
		}
		if row.PID == 3 && row.MemoryAvailable {
			t.Fatal("restricted memory marked available")
		}
	}
}

func TestTaskSnapshotSharesScanAndCancelsWait(t *testing.T) {
	sampler := newTaskSampler()
	sampler.prev = &taskSample{at: time.Now().Add(-time.Second), processes: map[int]taskCounter{}}
	var scans atomic.Int32
	sampler.read = func(context.Context) (*taskSample, error) {
		scans.Add(1)
		return &taskSample{at: time.Now(), processes: map[int]taskCounter{}}, nil
	}
	sampler.gpus = func(context.Context) []TaskGPU { return []TaskGPU{} }
	var calls sync.WaitGroup
	for range 12 {
		calls.Add(1)
		go func() {
			defer calls.Done()
			result, err := sampler.snapshot(context.Background(), false)
			if err != nil || result == nil || result.Processes == nil || result.GPUs == nil || result.Capabilities.GPU.Supported {
				t.Errorf("snapshot: %+v %v", result, err)
			}
		}()
	}
	calls.Wait()
	if scans.Load() != 1 {
		t.Fatalf("concurrent windows caused %d scans", scans.Load())
	}
	sampler.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := sampler.snapshot(ctx, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting for another caller ignored cancellation: %v", err)
	}
	<-sampler.gate
	if _, err := sampler.snapshot(ctx, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled cached request: %v", err)
	}
}

func TestTaskGPUUnknownIsNull(t *testing.T) {
	rows := parseNVIDIATaskGPUs("GPU-123, NVIDIA Test, 35, 128, 4096\nGPU-456, NVIDIA Unknown, N/A, N/A, 8192\n")
	if len(rows) != 2 || rows[0].Utilization == nil || *rows[0].Utilization != 35 || *rows[0].MemoryUsed != 128<<20 || rows[1].Utilization != nil || rows[1].MemoryUsed != nil {
		t.Fatalf("GPU counters: %+v", rows)
	}
	data, err := json.Marshal(rows[1])
	if err != nil || string(data) == "" {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil || decoded["utilization"] != nil || decoded["memoryUsed"] != nil {
		t.Fatalf("unknown values must serialize as null: %s", data)
	}
	for _, invalid := range []string{"N/A", "NaN", "+Inf", "-1", "101"} {
		if gpuPercent(invalid) != nil {
			t.Errorf("invalid GPU percent %q accepted", invalid)
		}
	}
	for _, invalid := range []string{"N/A", "NaN", "+Inf", "-1", "1e90"} {
		if gpuMemoryMiB(invalid) != nil {
			t.Errorf("invalid GPU memory %q accepted", invalid)
		}
	}
}

func TestTaskSnapshotCacheStartsAfterSlowGPUProbe(t *testing.T) {
	sampler := newTaskSampler()
	sampler.prev = &taskSample{at: time.Now().Add(-time.Second), processes: map[int]taskCounter{}}
	var scans, gpuQueries atomic.Int32
	sampler.read = func(context.Context) (*taskSample, error) {
		scans.Add(1)
		return &taskSample{at: time.Now(), processes: map[int]taskCounter{}}, nil
	}
	sampler.gpus = func(context.Context) []TaskGPU {
		gpuQueries.Add(1)
		// A real command timeout can exceed the one-second cache TTL.
		time.Sleep(1100 * time.Millisecond)
		return []TaskGPU{}
	}
	var calls sync.WaitGroup
	for range 6 {
		calls.Add(1)
		go func() {
			defer calls.Done()
			if _, err := sampler.snapshot(context.Background(), false); err != nil {
				t.Error(err)
			}
		}()
	}
	calls.Wait()
	if scans.Load() != 1 || gpuQueries.Load() != 1 {
		t.Fatalf("slow probe expired the cache before completion: scans=%d gpuQueries=%d", scans.Load(), gpuQueries.Load())
	}
}

func TestTaskManagerLiveReadOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := newTaskSampler().snapshot(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Processes) == 0 || result.Scope != "visible" || result.Source != "host" || result.Interval <= 0 || result.CPUCores < 1 {
		t.Fatalf("invalid live snapshot: %+v", result)
	}
}
