//go:build unix

package system

import (
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// uname returns the kernel release and the machine architecture.
func uname() (kernel, arch string, ok bool) {
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil {
		return "", "", false
	}
	return utsString(u.Release[:]), utsString(u.Machine[:]), true
}

func utsString(b []int8) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		out = append(out, byte(c))
	}
	return string(out)
}

// fillPlatform adds the details read from /proc, /sys and uname.
func fillPlatform(h *Host) {
	if kernel, arch, ok := uname(); ok {
		h.Kernel = kernel
		h.Arch = arch
	}
	if f, err := os.Open("/proc/cpuinfo"); err == nil {
		h.CPU = parseCPUInfo(io.LimitReader(f, 4<<20))
		f.Close()
	}
	if h.CPU.Threads == 0 {
		h.CPU.Threads = runtime.NumCPU()
	}
	if h.CPU.Cores == 0 {
		h.CPU.Cores = h.CPU.Threads
	}
	if mi, err := readMeminfo(); err == nil {
		h.MemoryTotal = mi["MemTotal"]
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if up, err := strconv.ParseFloat(f[0], 64); err == nil {
				h.Uptime = int64(up)
				h.BootTime = time.Now().Add(-time.Duration(up * float64(time.Second))).Unix()
			}
		}
	}
	h.Machine.Vendor = readTrim("/sys/class/dmi/id/sys_vendor")
	h.Machine.Product = readTrim("/sys/class/dmi/id/product_name")
}

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil || len(b) > 256 {
		return ""
	}
	return strings.TrimSpace(string(b))
}
