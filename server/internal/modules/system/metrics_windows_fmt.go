package system

import "errors"

var errNoCPU = errors.New("cpu times are not available")

// windowsMemory maps GlobalMemoryStatusEx figures to the API shapes. The page
// file figures there are the commit limit (RAM + page files), so swap is the
// commit limit minus physical memory.
func windowsMemory(physTotal, physAvail, commitTotal, commitAvail uint64) (Memory, Swap) {
	mem := Memory{Total: physTotal, Free: min(physAvail, physTotal), Available: min(physAvail, physTotal)}
	mem.Used = mem.Total - mem.Available
	mem.Percent = pct(mem.Used, mem.Total)
	var sw Swap
	if commitTotal > physTotal {
		sw.Total = commitTotal - physTotal
		// Commit available beyond free RAM is free page file.
		freePage := uint64(0)
		if commitAvail > physAvail {
			freePage = commitAvail - physAvail
		}
		sw.Free = min(freePage, sw.Total)
		sw.Used = sw.Total - sw.Free
		sw.Percent = pct(sw.Used, sw.Total)
	}
	return mem, sw
}
