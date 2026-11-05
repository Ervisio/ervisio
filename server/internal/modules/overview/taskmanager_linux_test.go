//go:build linux

package overview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskManagerAllVisibleProcessesAndCancellation(t *testing.T) {
	root := t.TempDir()
	for pid := 1; pid <= 151; pid++ {
		dir := filepath.Join(root, fmt.Sprint(pid))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if pid == 151 {
			continue
		} // visible, but counters unavailable
		stat := fmt.Sprintf("%d (worker (test)) S 1 1 1 0 0 0 0 0 0 0 150 50 0 0 20 0 5 0 100 1000000 777 0", pid)
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sample, err := readLinuxTaskSample(context.Background(), root)
	if err != nil || len(sample.processes) != 151 {
		t.Fatalf("all processes (not legacy cap100): %d %v", len(sample.processes), err)
	}
	row := sample.processes[1]
	if row.row.Threads != 5 || row.row.PPID != 1 || row.started != 100 || !row.row.MemoryAvailable || row.row.Memory != 777*uint64(os.Getpagesize()) {
		t.Fatalf("process counters: %+v", row)
	}
	if sample.processes[151].row.MemoryAvailable || sample.processes[151].row.CPUAvailable {
		t.Fatal("restricted process presented as measurable zero")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readLinuxTaskSample(ctx, root); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestTaskDRMSensorsReadOnly(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "card0", "device")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"gpu_busy_percent": "0\n", "mem_info_vram_used": "1024", "mem_info_vram_total": "4096"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "card0-HDMI-A-1"), 0700); err != nil {
		t.Fatal(err)
	}
	rows := readDRMTaskGPUs(context.Background(), root)
	if len(rows) != 1 || rows[0].Utilization == nil || *rows[0].Utilization != 0 || *rows[0].MemoryTotal != 4096 {
		t.Fatalf("GPU sensor rows: %+v", rows)
	}
}
