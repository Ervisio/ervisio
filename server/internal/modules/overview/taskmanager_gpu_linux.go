//go:build linux

package overview

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/sys"
)

func readTaskGPUs(ctx context.Context) []TaskGPU {
	result := readDRMTaskGPUs(ctx, "/sys/class/drm")
	if _, err := sys.LookPath("nvidia-smi"); err == nil && ctx.Err() == nil {
		data, err := (sys.Cmd{Name: "nvidia-smi", Args: []string{"--query-gpu=uuid,name,utilization.gpu,memory.used,memory.total", "--format=csv,noheader,nounits"}, Timeout: 2 * time.Second, MaxOutput: 64 << 10}).Output(ctx)
		if err == nil {
			result = append(result, parseNVIDIATaskGPUs(string(data))...)
		}
	}
	return result
}

func readDRMTaskGPUs(ctx context.Context, root string) []TaskGPU {
	result := []TaskGPU{}
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		index, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), "card"))
		if err != nil || index < 0 || !strings.HasPrefix(entry.Name(), "card") {
			continue
		}
		device := filepath.Join(root, entry.Name(), "device")
		busy, _ := os.ReadFile(filepath.Join(device, "gpu_busy_percent"))
		gpu := TaskGPU{ID: entry.Name(), Name: entry.Name(), Utilization: gpuPercent(string(busy))}
		readBytes := func(name string) *uint64 {
			data, err := os.ReadFile(filepath.Join(device, name))
			if err != nil {
				return nil
			}
			n, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
			if err != nil {
				return nil
			}
			return &n
		}
		gpu.MemoryUsed = readBytes("mem_info_vram_used")
		gpu.MemoryTotal = readBytes("mem_info_vram_total")
		if gpu.Utilization != nil || gpu.MemoryUsed != nil || gpu.MemoryTotal != nil {
			result = append(result, gpu)
		}
	}
	return result
}
