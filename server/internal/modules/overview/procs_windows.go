//go:build windows

package overview

import (
	"context"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ervisio/ervisio/server/internal/sys"
	"golang.org/x/sys/windows"
)

var (
	modPsapi                 = windows.NewLazySystemDLL("psapi.dll")
	procK32GetProcessMemInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")
	procGetProcessMemInfo    = modPsapi.NewProc("GetProcessMemoryInfo")
)

type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

func workingSet(h windows.Handle) (uint64, bool) {
	var m processMemoryCounters
	m.cb = uint32(unsafe.Sizeof(m))
	proc := procK32GetProcessMemInfo
	if proc.Find() != nil {
		proc = procGetProcessMemInfo
	}
	r, _, _ := proc.Call(uintptr(h), uintptr(unsafe.Pointer(&m)), uintptr(m.cb))
	return uint64(m.WorkingSetSize), r != 0
}

func filetime100ns(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

// winSample takes one toolhelp snapshot and reads times and memory for every
// process that can be opened; others still appear with name and pid.
func winSample() []winProc {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	if windows.Process32First(snap, &e) != nil {
		return nil
	}
	var out []winProc
	for {
		p := winProc{pid: int(e.ProcessID), ppid: int(e.ParentProcessID), name: windows.UTF16ToString(e.ExeFile[:]), threads: e.Threads}
		if p.pid != 0 {
			if h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, e.ProcessID); err == nil {
				var c, x, k, u windows.Filetime
				if windows.GetProcessTimes(h, &c, &x, &k, &u) == nil {
					p.cpu100ns = filetime100ns(k) + filetime100ns(u)
				}
				if ws, ok := workingSet(h); ok {
					p.rss = ws
				}
				windows.CloseHandle(h)
			}
		}
		out = append(out, p)
		if windows.Process32Next(snap, &e) != nil {
			break
		}
	}
	return out
}

var (
	ownerMu    sync.Mutex
	ownerCache = map[string]string{}
)

// winDescribe returns the image path and owner of a process (best effort).
func winDescribe(pid int, name string) (cmd, owner string) {
	if pid != 0 {
		if h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)); err == nil {
			defer windows.CloseHandle(h)
			buf := make([]uint16, windows.MAX_LONG_PATH)
			n := uint32(len(buf))
			if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
				cmd = windows.UTF16ToString(buf[:n])
			}
			owner = processOwner(h)
		}
	}
	if cmd == "" {
		cmd = "[" + name + "]"
	}
	if len(cmd) > 300 {
		cmd = cmd[:300]
	}
	return
}

func processOwner(h windows.Handle) string {
	var tok windows.Token
	if windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok) != nil {
		return ""
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return ""
	}
	key := u.User.Sid.String()
	ownerMu.Lock()
	defer ownerMu.Unlock()
	if s, ok := ownerCache[key]; ok {
		return s
	}
	s := key
	if acct, dom, _, err := u.User.Sid.LookupAccount(""); err == nil {
		s = acct
		if dom != "" {
			s = dom + `\` + acct
		}
	}
	ownerCache[key] = s
	return strings.TrimSpace(s)
}

func windowsProcesses(ctx context.Context, by string, limit int) ([]Process, error) {
	winPrev.Lock()
	defer winPrev.Unlock()
	var prev map[int]uint64
	var t0 time.Time
	if usablePrev(winPrev.at, time.Now()) {
		prev, t0 = winPrev.cpu, winPrev.at
	} else {
		t0 = time.Now()
		prev = cpuMap(winSample())
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
	cur := winSample()
	now := time.Now()
	winPrev.at, winPrev.cpu = now, cpuMap(cur)
	var total uint64
	if m, ok := sys.MemoryStatus(); ok {
		total = m.PhysTotal
	}
	list := rankProcesses(winProcesses(prev, now.Sub(t0), cur, total), by, limit)
	for i := range list {
		list[i].Command, list[i].User = winDescribe(list[i].PID, list[i].Name)
	}
	return list, nil
}
