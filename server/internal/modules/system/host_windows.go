//go:build windows

package system

import (
	"runtime"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/sys"
)

const ntKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`

// fillPlatform adds the details read from the registry and the Win32 API.
func fillPlatform(h *Host) {
	h.Kernel = windowsKernel(sys.RegString(ntKey, "CurrentBuild"), sys.RegUint(ntKey, "UBR"))
	h.CPU.Model = strings.TrimSpace(sys.RegString(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "ProcessorNameString"))
	h.CPU.Threads = runtime.NumCPU()
	h.CPU.Cores = h.CPU.Threads
	if m, ok := sys.MemoryStatus(); ok {
		h.MemoryTotal = m.PhysTotal
	}
	if up, ok := sys.UptimeSeconds(); ok {
		h.Uptime = up
		h.BootTime = time.Now().Add(-time.Duration(up) * time.Second).Unix()
	}
	h.Machine.Vendor = strings.TrimSpace(sys.RegString(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemManufacturer"))
	h.Machine.Product = strings.TrimSpace(sys.RegString(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemProductName"))
}
