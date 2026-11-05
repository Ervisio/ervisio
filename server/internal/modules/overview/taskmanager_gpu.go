package overview

import (
	"encoding/csv"
	"math"
	"strconv"
	"strings"
)

func gpuPercent(value string) *float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 100 {
		return nil
	}
	return &n
}

func gpuMemoryMiB(value string) *uint64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n >= float64(^uint64(0))/(1<<20) {
		return nil
	}
	bytes := uint64(n * (1 << 20))
	return &bytes
}

func parseNVIDIATaskGPUs(data string) []TaskGPU {
	rows, err := csv.NewReader(strings.NewReader(data)).ReadAll()
	if err != nil {
		return nil
	}
	result := []TaskGPU{}
	for _, row := range rows {
		if len(row) != 5 || strings.TrimSpace(row[0]) == "" {
			continue
		}
		gpu := TaskGPU{ID: "nvidia:" + strings.TrimSpace(row[0]), Name: strings.TrimSpace(row[1]), Utilization: gpuPercent(row[2]), MemoryUsed: gpuMemoryMiB(row[3]), MemoryTotal: gpuMemoryMiB(row[4])}
		if gpu.Utilization != nil || gpu.MemoryUsed != nil || gpu.MemoryTotal != nil {
			result = append(result, gpu)
		}
	}
	return result
}
