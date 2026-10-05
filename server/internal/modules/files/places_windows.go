//go:build windows

package files

import (
	"sort"

	"golang.org/x/sys/windows"
)

// readDisks lists the fixed drives (C:\, D:\, ...) with their capacity.
func readDisks() []disk {
	out := []disk{}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return out
	}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		p, err := windows.UTF16PtrFromString(root)
		if err != nil || windows.GetDriveType(p) != windows.DRIVE_FIXED {
			continue
		}
		var avail, total, free uint64
		if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil || total == 0 {
			continue
		}
		label := make([]uint16, windows.MAX_PATH+1)
		fsn := make([]uint16, windows.MAX_PATH+1)
		dev := root
		fs := ""
		if windows.GetVolumeInformation(p, &label[0], uint32(len(label)), nil, nil, nil, &fsn[0], uint32(len(fsn))) == nil {
			fs = windows.UTF16ToString(fsn)
			if l := windows.UTF16ToString(label); l != "" {
				dev = l
			}
		}
		out = append(out, disk{Mount: root, Device: dev, FS: fs, Total: total, Used: total - free, Avail: avail})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Mount < out[j].Mount })
	return out
}
