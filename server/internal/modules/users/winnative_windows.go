//go:build windows

package users

// Native (netapi32) read path for local accounts. Unlike the PowerShell
// cmdlets it needs no user profile and no module loading, and the Net*
// enumeration calls work for unprivileged local callers.

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

var (
	netapi32                   = windows.NewLazySystemDLL("netapi32.dll")
	procNetUserEnum            = netapi32.NewProc("NetUserEnum")
	procNetLocalGroupEnum      = netapi32.NewProc("NetLocalGroupEnum")
	procNetLocalGroupGetMember = netapi32.NewProc("NetLocalGroupGetMembers")
	procNetApiBufferFree       = netapi32.NewProc("NetApiBufferFree")
)

const (
	nerrMoreData     = 234
	maxPreferredLen  = 0xFFFFFFFF
	filterNormalAcct = 0x0002
)

type userInfo3 struct {
	Name            *uint16
	Password        *uint16
	PasswordAge     uint32
	Priv            uint32
	HomeDir         *uint16
	Comment         *uint16
	Flags           uint32
	ScriptPath      *uint16
	AuthFlags       uint32
	FullName        *uint16
	UsrComment      *uint16
	Parms           *uint16
	Workstations    *uint16
	LastLogon       uint32
	LastLogoff      uint32
	AcctExpires     uint32
	MaxStorage      uint32
	UnitsPerWeek    uint32
	LogonHours      *byte
	BadPwCount      uint32
	NumLogons       uint32
	LogonServer     *uint16
	CountryCode     uint32
	CodePage        uint32
	UserID          uint32
	PrimaryGroupID  uint32
	Profile         *uint16
	HomeDirDrive    *uint16
	PasswordExpired uint32
}

type localGroupInfo1 struct {
	Name    *uint16
	Comment *uint16
}

type localGroupMembersInfo2 struct {
	SID           *windows.SID
	SIDUsage      uint32
	DomainAndName *uint16
}

func netFree(p uintptr) {
	if p != 0 {
		procNetApiBufferFree.Call(p)
	}
}

// netEnum drives a Net*Enum style call, handing each page's buffer to visit.
func netEnum(call func(resume *uintptr, buf *uintptr, n, total *uint32) uint32, visit func(base unsafe.Pointer, n int)) uint32 {
	var resume uintptr
	for {
		var buf uintptr
		var n, total uint32
		st := call(&resume, &buf, &n, &total)
		if st != 0 && st != nerrMoreData {
			netFree(buf)
			return st
		}
		visit(*(*unsafe.Pointer)(unsafe.Pointer(&buf)), int(n))
		netFree(buf)
		if st == 0 {
			return 0
		}
	}
}

func sidOf(name string) string {
	sid, _, _, err := windows.LookupSID("", name)
	if err != nil || sid == nil {
		return ""
	}
	return sid.String()
}

func profilePath(sid string) string {
	if sid == "" {
		return ""
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\`+sid, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("ProfileImagePath")
	if err != nil {
		return ""
	}
	if e, err := registry.ExpandString(v); err == nil {
		return e
	}
	return v
}

func nativeUsers(now time.Time) ([]winUser, error) {
	var users []winUser
	st := netEnum(func(resume *uintptr, buf *uintptr, n, total *uint32) uint32 {
		r, _, _ := procNetUserEnum.Call(0, 3, filterNormalAcct, uintptr(unsafe.Pointer(buf)),
			maxPreferredLen, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(total)), uintptr(unsafe.Pointer(resume)))
		return uint32(r)
	}, func(base unsafe.Pointer, n int) {
		for i := 0; i < n; i++ {
			ui := (*userInfo3)(unsafe.Add(base, uintptr(i)*unsafe.Sizeof(userInfo3{})))
			nu := nativeUser{
				Name: windows.UTF16PtrToString(ui.Name), FullName: windows.UTF16PtrToString(ui.FullName),
				Comment: windows.UTF16PtrToString(ui.Comment), Flags: ui.Flags, PasswordAge: ui.PasswordAge,
				LastLogon: ui.LastLogon, AcctExpires: ui.AcctExpires,
			}
			sid := sidOf(nu.Name)
			users = append(users, nu.toWinUser(sid, profilePath(sid), now))
		}
	})
	if st != 0 {
		return nil, netStatusError("Could not read the accounts", st, "NetUserEnum failed")
	}
	return users, nil
}

func nativeGroupMembers(group string) []winMember {
	name, err := windows.UTF16PtrFromString(group)
	if err != nil {
		return nil
	}
	var members []winMember
	netEnum(func(resume *uintptr, buf *uintptr, n, total *uint32) uint32 {
		r, _, _ := procNetLocalGroupGetMember.Call(0, uintptr(unsafe.Pointer(name)), 2, uintptr(unsafe.Pointer(buf)),
			maxPreferredLen, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(total)), uintptr(unsafe.Pointer(resume)))
		return uint32(r)
	}, func(base unsafe.Pointer, n int) {
		for i := 0; i < n; i++ {
			mi := (*localGroupMembersInfo2)(unsafe.Add(base, uintptr(i)*unsafe.Sizeof(localGroupMembersInfo2{})))
			sid := ""
			if mi.SID != nil {
				sid = mi.SID.String()
			}
			members = append(members, winMember{Name: windows.UTF16PtrToString(mi.DomainAndName), SID: sid})
		}
	})
	return members // unreadable groups yield no members, like the script path
}

func nativeGroups() ([]winGroup, error) {
	var groups []winGroup
	st := netEnum(func(resume *uintptr, buf *uintptr, n, total *uint32) uint32 {
		r, _, _ := procNetLocalGroupEnum.Call(0, 1, uintptr(unsafe.Pointer(buf)),
			maxPreferredLen, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(total)), uintptr(unsafe.Pointer(resume)))
		return uint32(r)
	}, func(base unsafe.Pointer, n int) {
		for i := 0; i < n; i++ {
			gi := (*localGroupInfo1)(unsafe.Add(base, uintptr(i)*unsafe.Sizeof(localGroupInfo1{})))
			name := windows.UTF16PtrToString(gi.Name)
			groups = append(groups, winGroup{
				Name: name, SID: sidOf(name), Description: windows.UTF16PtrToString(gi.Comment),
				Members: nativeGroupMembers(name),
			})
		}
	})
	if st != 0 {
		return nil, netStatusError("Could not read the groups", st, "NetLocalGroupEnum failed")
	}
	return groups, nil
}

func nativeSnapshot(now time.Time) (*winSnapshot, error) {
	users, err := nativeUsers(now)
	if err != nil {
		return nil, err
	}
	groups, err := nativeGroups()
	if err != nil {
		return nil, err
	}
	computer, _ := windows.ComputerName()
	s := &winSnapshot{Computer: computer, Users: users, Groups: groups}
	if tu, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil {
		s.Self = tu.User.Sid.String()
		if acc, _, _, err := tu.User.Sid.LookupAccount(""); err == nil {
			s.SelfName = acc
		}
	}
	if s.Self == "" {
		return nil, rpc.Errorf(rpc.Internal, "Could not read the accounts: %s", fmt.Sprint("no token user"))
	}
	sort.SliceStable(s.Users, func(i, j int) bool { return strings.ToLower(s.Users[i].Name) < strings.ToLower(s.Users[j].Name) })
	return s, nil
}
