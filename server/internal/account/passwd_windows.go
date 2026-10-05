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

// PasswordSetFingerprint returns the account's password-last-set time as a
// decimal Unix-seconds string (NetUserGetInfo level 3, usri3_password_age:
// last set = now - age), the Windows counterpart of the shadow fingerprint.
// It changes when the password is changed. Only local accounts are
// supported: for domain accounts it returns ErrNoShadowEntry (their
// password age lives on a domain controller and is not checked here).
func PasswordSetFingerprint(name string) (string, error) {
	n, ok := localName(name)
	if !ok {
		return "", ErrNoShadowEntry
	}
	pn, err := windows.UTF16PtrFromString(n)
	if err != nil {
		return "", err
	}
	var buf *byte
	r, _, _ := procNetUserGetInfo.Call(0, uintptr(unsafe.Pointer(pn)), 3, uintptr(unsafe.Pointer(&buf)))
	if r != 0 {
		// NERR_UserNotFound (2221) and the like: not a local account.
		return "", ErrNoShadowEntry
	}
	defer procNetApiBufferFree.Call(uintptr(unsafe.Pointer(buf)))
	info := (*userInfo3Head)(unsafe.Pointer(buf))
	last := time.Now().Add(-time.Duration(info.PasswordAge) * time.Second)
	return strconv.FormatInt(last.Unix(), 10), nil
}
