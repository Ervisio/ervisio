//go:build windows

package account

import (
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modNetapi32          = windows.NewLazySystemDLL("netapi32.dll")
	procNetUserGetInfo   = modNetapi32.NewProc("NetUserGetInfo")
	procNetApiBufferFree = modNetapi32.NewProc("NetApiBufferFree")
	procDsGetDcName      = modNetapi32.NewProc("DsGetDcNameW")
)

// userInfo3Head is the leading part of USER_INFO_3 (up to
// usri3_password_age); only these fields are read.
type userInfo3Head struct {
	Name        *uint16
	Password    *uint16
	PasswordAge uint32
}

// localName returns the SAM user name for a local account ("user" or
// "THISHOST\user" / ".\user"), or ok=false for domain accounts.
func localName(name string) (string, bool) {
	if strings.Contains(name, "@") {
		return "", false
	}
	if i := strings.IndexByte(name, '\\'); i >= 0 {
		dom := name[:i]
		host, _ := os.Hostname()
		if dom != "." && !strings.EqualFold(dom, host) {
			return "", false
		}
		return name[i+1:], true
	}
	return name, true
}

// dcInfoHead is the leading part of DOMAIN_CONTROLLER_INFOW (DomainControllerName).
type dcInfoHead struct {
	DomainControllerName *uint16
}

const (
	dsReturnDNSName  = 0x40000000
	dsReturnFlatName = 0x80000000
)

// domainController resolves a DC for the domain, returning "\\\\dc" form.
func domainController(domain string, dns bool) (string, error) {
	pd, err := windows.UTF16PtrFromString(domain)
	if err != nil {
		return "", err
	}
	flags := uintptr(dsReturnDNSName)
	if !dns {
		flags = uintptr(dsReturnFlatName)
	}
	var info *dcInfoHead
	r, _, _ := procDsGetDcName.Call(0, uintptr(unsafe.Pointer(pd)), 0, 0, flags, uintptr(unsafe.Pointer(&info)))
	if r != 0 || info == nil {
		return "", ErrNoShadowEntry
	}
	defer procNetApiBufferFree.Call(uintptr(unsafe.Pointer(info)))
	if info.DomainControllerName == nil {
		return "", ErrNoShadowEntry
	}
	return windows.UTF16PtrToString(info.DomainControllerName), nil
}

// passwordAge queries NetUserGetInfo level 3 on server ("" = local).
func passwordAge(server, user string) (string, error) {
	pu, err := windows.UTF16PtrFromString(user)
	if err != nil {
		return "", err
	}
	var ps uintptr
	if server != "" {
		p, err := windows.UTF16PtrFromString(server)
		if err != nil {
			return "", err
		}
		ps = uintptr(unsafe.Pointer(p))
	}
	var buf *byte
	r, _, _ := procNetUserGetInfo.Call(ps, uintptr(unsafe.Pointer(pu)), 3, uintptr(unsafe.Pointer(&buf)))
	if r != 0 || buf == nil {
		// NERR_UserNotFound (2221), access denied, DC unreachable...
		return "", ErrNoShadowEntry
	}
	defer procNetApiBufferFree.Call(uintptr(unsafe.Pointer(buf)))
	info := (*userInfo3Head)(unsafe.Pointer(buf))
	last := time.Now().Add(-time.Duration(info.PasswordAge) * time.Second)
	return strconv.FormatInt(last.Unix(), 10), nil
}

// isLocalDomain reports whether the domain part names this computer.
func isLocalDomain(domain string) bool {
	host, _ := os.Hostname()
	if host == "" {
		return false
	}
	d := strings.ToLower(domain)
	h := strings.ToLower(host)
	return d == "." || d == h || strings.HasPrefix(d, h+".")
}

// PasswordSetFingerprint returns the account's password-last-set time as a
// decimal Unix-seconds string (NetUserGetInfo level 3, usri3_password_age:
// last set = now - age), the Windows counterpart of the shadow fingerprint.
// Local accounts are queried locally; domain accounts ("DOMAIN\\user",
// "user@domain") are queried on a domain controller found with DsGetDcNameW.
// On any failure it returns ErrNoShadowEntry (callers fall back to the
// account check).
func PasswordSetFingerprint(name string) (string, error) {
	if n, ok := localName(name); ok {
		return passwordAge("", n)
	}
	domain, user, dns, ok := splitDomainAccount(name)
	if !ok {
		return "", ErrNoShadowEntry
	}
	if isLocalDomain(domain) {
		// Includes a machine that is itself the DC for its own name.
		return passwordAge("", user)
	}
	dc, err := domainController(domain, dns)
	if err != nil {
		return "", ErrNoShadowEntry
	}
	return passwordAge(dc, user)
}
