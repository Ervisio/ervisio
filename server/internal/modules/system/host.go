package system

import (
	"bufio"
	"io"
	"runtime"
	"strings"

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
	fillPlatform(h)
	return h, nil
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
