package remote

import (
	"math"
	"testing"
)

func TestCapacityUsesReportedBlocksAndRejectsInvalidOrOverflowedValues(t *testing.T) {
	space := capacityFromBlocks("/backup", 4096, 100, 30, 20)
	if !space.Supported || space.Total != 409600 || space.Used != 286720 || space.Free != 122880 || space.Avail != 81920 {
		t.Fatalf("wrong space: %+v", space)
	}
	for _, args := range [][4]uint64{{0, 100, 30, 20}, {4096, 0, 0, 0}, {4096, 20, 30, 10}, {4096, 100, 20, 30}, {4096, math.MaxUint64, 1, 1}} {
		result := capacityFromBlocks("/", args[0], args[1], args[2], args[3])
		if result.Supported || result.Total != 0 {
			t.Fatalf("invalid statistics were presented as capacity: %+v", result)
		}
	}
}
