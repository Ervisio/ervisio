//go:build windows

package sys

import (
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")
	modIphlpapi = windows.NewLazySystemDLL("iphlpapi.dll")

	procGlobalMemoryStatusEx = modKernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes       = modKernel32.NewProc("GetSystemTimes")
	procGetTickCount64       = modKernel32.NewProc("GetTickCount64")
	procGetIfTable2          = modIphlpapi.NewProc("GetIfTable2")
	procFreeMibTable         = modIphlpapi.NewProc("FreeMibTable")
)

// RegString reads a string value under HKLM; "" when missing.
func RegString(path, name string) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return v
}

// MemStatus is the result of GlobalMemoryStatusEx (bytes).
type MemStatus struct {
	PhysTotal, PhysAvail, PageTotal, PageAvail uint64
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// MemoryStatus returns physical memory and commit-limit figures.
func MemoryStatus() (MemStatus, bool) {
	m := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return MemStatus{}, false
	}
	return MemStatus{m.TotalPhys, m.AvailPhys, m.TotalPageFile, m.AvailPageFile}, true
}

// SystemTimes returns the aggregate idle and total (kernel+user, kernel
// includes idle) CPU times in 100ns units.
func SystemTimes() (idle, total uint64, ok bool) {
	var i, k, u windows.Filetime
	r, _, _ := procGetSystemTimes.Call(uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))
	if r == 0 {
		return 0, 0, false
	}
	ft := func(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
	return ft(i), ft(k) + ft(u), true
}

type procPerf struct {
	Idle, Kernel, User, Dpc, Interrupt int64
	InterruptCount                     uint32
	_                                  uint32
}

// CoreTimes returns per-logical-processor idle and total times (100ns).
func CoreTimes(n int) (idle, total []uint64, ok bool) {
	if n <= 0 {
		return nil, nil, false
	}
	buf := make([]procPerf, n)
	var ret uint32
	const systemProcessorPerformanceInformation = 8
	if err := windows.NtQuerySystemInformation(systemProcessorPerformanceInformation, unsafe.Pointer(&buf[0]),
		uint32(len(buf))*uint32(unsafe.Sizeof(buf[0])), &ret); err != nil {
		return nil, nil, false
	}
	cnt := int(ret) / int(unsafe.Sizeof(buf[0]))
	for _, p := range buf[:min(cnt, n)] {
		idle = append(idle, uint64(p.Idle))
		total = append(total, uint64(p.Kernel)+uint64(p.User))
	}
	return idle, total, len(idle) > 0
}

// UptimeSeconds returns the seconds since boot (GetTickCount64).
func UptimeSeconds() (int64, bool) {
	r, _, _ := procGetTickCount64.Call()
	if r == 0 {
		return 0, false
	}
	return int64(r / 1000), true
}

// FixedDrives lists the roots (like `C:\`) of fixed local drives.
func FixedDrives() []string {
	buf := make([]uint16, 512)
	n, err := windows.GetLogicalDriveStrings(uint32(len(buf)), &buf[0])
	if err != nil || n == 0 || int(n) > len(buf) {
		return nil
	}
	var out []string
	for _, s := range splitMultiSZ(buf[:n]) {
		p, err := windows.UTF16PtrFromString(s)
		if err != nil {
			continue
		}
		if windows.GetDriveType(p) == windows.DRIVE_FIXED {
			out = append(out, s)
		}
	}
	return out
}

// DiskSpace returns total and free bytes (avail is what the caller can use).
func DiskSpace(path string) (total, free, avail uint64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, 0, err
	}
	err = windows.GetDiskFreeSpaceEx(p, &avail, &total, &free)
	return
}

// VolumeFS returns the file system name (NTFS, ReFS, ...) of a drive root.
func VolumeFS(root string) string {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return ""
	}
	name := make([]uint16, 64)
	if windows.GetVolumeInformation(p, nil, 0, nil, nil, nil, &name[0], uint32(len(name))) != nil {
		return ""
	}
	return windows.UTF16ToString(name)
}

// IfCounter is one network interface with its octet counters.
type IfCounter struct {
	Name     string
	Rx, Tx   uint64
	Hardware bool
	Up       bool
}

// ifRow2 mirrors MIB_IF_ROW2 (1352 bytes on 32 and 64 bit).
type ifRow2 struct {
	InterfaceLuid            uint64
	InterfaceIndex           uint32
	InterfaceGuid            windows.GUID
	Alias                    [257]uint16
	Description              [257]uint16
	PhysicalAddressLength    uint32
	PhysicalAddress          [32]byte
	PermanentPhysicalAddress [32]byte
	Mtu                      uint32
	Type                     uint32
	TunnelType               uint32
	MediaType                uint32
	PhysicalMediumType       uint32
	AccessType               uint32
	DirectionType            uint32
	Flags                    uint8
	OperStatus               uint32
	AdminStatus              uint32
	MediaConnectState        uint32
	NetworkGuid              windows.GUID
	ConnectionType           uint32
	TransmitLinkSpeed        uint64
	ReceiveLinkSpeed         uint64
	InOctets                 uint64
	InUcastPkts              uint64
	InNUcastPkts             uint64
	InDiscards               uint64
	InErrors                 uint64
	InUnknownProtos          uint64
	InUcastOctets            uint64
	InMulticastOctets        uint64
	InBroadcastOctets        uint64
	OutOctets                uint64
	OutUcastPkts             uint64
	OutNUcastPkts            uint64
	OutDiscards              uint64
	OutErrors                uint64
	OutUcastOctets           uint64
	OutMulticastOctets       uint64
	OutBroadcastOctets       uint64
	OutQLen                  uint64
}

// Compile-time size check against MIB_IF_ROW2.
var (
	_ [1352 - unsafe.Sizeof(ifRow2{})]byte
	_ [unsafe.Sizeof(ifRow2{}) - 1352]byte
)

// IfCounters reads interface octet counters through GetIfTable2.
func IfCounters() ([]IfCounter, bool) {
	var tbl unsafe.Pointer
	if r, _, _ := procGetIfTable2.Call(uintptr(unsafe.Pointer(&tbl))); r != 0 || tbl == nil {
		return nil, false
	}
	defer procFreeMibTable.Call(uintptr(tbl))
	n := *(*uint32)(tbl)
	rows := unsafe.Slice((*ifRow2)(unsafe.Add(tbl, 8)), n)
	var out []IfCounter
	for i := range rows {
		r := &rows[i]
		if r.Type == 24 { // software loopback
			continue
		}
		name := windows.UTF16ToString(r.Alias[:])
		if name == "" {
			name = windows.UTF16ToString(r.Description[:])
		}
		out = append(out, IfCounter{Name: name, Rx: r.InOctets, Tx: r.OutOctets, Hardware: r.Flags&1 != 0, Up: r.OperStatus == 1})
	}
	return out, true
}

func splitMultiSZ(b []uint16) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c == 0 {
			if i > start {
				out = append(out, windows.UTF16ToString(b[start:i]))
			}
			start = i + 1
		}
	}
	return out
}

// RegUint reads a DWORD value under HKLM; "" when missing.
func RegUint(path, name string) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue(name)
	if err != nil {
		return ""
	}
	return strconv.FormatUint(v, 10)
}
