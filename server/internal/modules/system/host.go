package system

import (
	"bufio"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/sys"
)

// Distro is the distribution summary.
type Distro struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PrettyName string `json:"prettyName"`
	Version    string `json:"version,omitempty"`
	Color      string `json:"color"`
	Logo       string `json:"logo,omitempty"`
}

// CPUInfo describes the processor.
type CPUInfo struct {
	Model   string `json:"model"`
	Cores   int    `json:"cores"`
	Threads int    `json:"threads"`
}

// Machine is the DMI vendor/product, when available.
type Machine struct {
	Vendor  string `json:"vendor,omitempty"`
	Product string `json:"product,omitempty"`
}

// Host is the result of system.host.
type Host struct {
	Hostname    string  `json:"hostname"`
	Distro      Distro  `json:"distro"`
	Kernel      string  `json:"kernel"`
	Arch        string  `json:"arch"`
	CPU         CPUInfo `json:"cpu"`
	MemoryTotal uint64  `json:"memoryTotal"`
	Uptime      int64   `json:"uptime"`   // seconds
	BootTime    int64   `json:"bootTime"` // unix seconds
	IP          string  `json:"ip,omitempty"`
	Machine     Machine `json:"machine"`
}

func readHost() (*Host, error) {
	osr := sys.ReadOSRelease()
	h := &Host{
		Hostname: sys.Hostname(),
		Distro: Distro{ID: osr.ID, Name: osr.Name, PrettyName: osr.PrettyName, Version: osr.VersionID,
			Color: sys.DistroColor(osr.ID), Logo: osr.Logo},
		Arch: runtime.GOARCH,
		IP:   sys.PrimaryIP(),
	}
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
	return h, nil
}

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil || len(b) > 256 {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func parseCPUInfo(r io.Reader) CPUInfo {
	var ci CPUInfo
	cores := map[string]bool{}
	var physID, coreID string
	flush := func() {
		if coreID != "" {
			cores[physID+"/"+coreID] = true
		}
		physID, coreID = "", ""
	}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "processor":
			ci.Threads++
		case "model name", "Model", "Hardware", "cpu model":
			if ci.Model == "" {
				ci.Model = v
			}
		case "physical id":
			physID = v
		case "core id":
			coreID = v
		}
	}
	flush()
	ci.Cores = len(cores)
	return ci
}
