//go:build windows

package pam

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Passwordless sign-in (SSH key): S4U ("service for user") logon. The
// daemon, running as SYSTEM with SeTcbPrivilege, asks the LSA for a token of
// the user without a password: MSV1_0_S4U_LOGON for local accounts,
// KERB_S4U_LOGON for domain accounts. The token is a network-type logon
// (no network credentials, no loaded profile) turned into a primary token,
// good for CreateProcessAsUser like the LogonUser one. The caller has
// already proven the key; this only produces the token.

const (
	s4uMessageType = 12 // MsV1_0S4ULogon == KerbS4ULogon
	logonNetwork   = 3
)

type lsaString struct {
	Length, MaximumLength uint16
	Buffer                *byte
}

type tokenSource struct {
	Name [8]byte
	ID   windows.LUID
}

var (
	secur32                  = windows.NewLazySystemDLL("secur32.dll")
	procLsaRegisterLogonProc = secur32.NewProc("LsaRegisterLogonProcess")
	procLsaLookupAuthPackage = secur32.NewProc("LsaLookupAuthenticationPackage")
	procLsaLogonUser         = secur32.NewProc("LsaLogonUser")
	procLsaFreeReturnBuffer  = secur32.NewProc("LsaFreeReturnBuffer")
	procLsaDeregister        = secur32.NewProc("LsaDeregisterLogonProcess")
	procAllocLUID            = windows.NewLazySystemDLL("advapi32.dll").NewProc("AllocateLocallyUniqueId")
	s4uMu                    sync.Mutex
)

func newLsaString(s string) (lsaString, []byte) {
	b := append([]byte(s), 0)
	return lsaString{Length: uint16(len(s)), MaximumLength: uint16(len(b)), Buffer: &b[0]}, b
}

func ntErr(op string, r uintptr) error {
	if r == 0 {
		return nil
	}
	return fmt.Errorf("pam: %s: %w", op, windows.NTStatus(r).Errno())
}

// enableTCB turns SeTcbPrivilege on in the process token (LsaRegisterLogonProcess
// needs it; the SYSTEM service holds it disabled).
func enableTCB() error {
	var t windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &t); err != nil {
		return err
	}
	defer t.Close()
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr("SeTcbPrivilege"), &luid); err != nil {
		return err
	}
	tp := windows.Tokenprivileges{PrivilegeCount: 1}
	tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	if err := windows.AdjustTokenPrivileges(t, false, &tp, 0, nil, nil); err != nil {
		return err
	}
	// A privilege not held makes LsaRegisterLogonProcess fail, with a clear status.
	return nil
}

// AuthenticateS4U obtains the user's token without a password and keeps it
// for UserToken, as Authenticate does. It must only be called after the
// caller proved the user's identity another way (an SSH key). The user must
// exist; disabled or expired accounts are refused by the LSA (ErrAccount).
func AuthenticateS4U(user string) error {
	if user == "" || !validInput(user) {
		return &Error{Kind: ErrAuth, Msg: "invalid input"}
	}
	name, domain, ok := splitUser(user)
	if !ok {
		return &Error{Kind: ErrAuth, Msg: "invalid user name"}
	}
	if _, have := lookupToken(user); have {
		return nil
	}
	local := domain == "." || domain == ""
	if !local {
		if cn, err := windows.ComputerName(); err == nil && strings.EqualFold(cn, domain) {
			local = true
		}
	}
	pkg, upn, realm := "MICROSOFT_AUTHENTICATION_PACKAGE_V1_0", name, domain
	if local {
		if strings.Contains(name, "@") { // a UPN is a domain account
			local = false
		} else if cn, err := windows.ComputerName(); err == nil {
			realm = cn
		}
	}
	if !local {
		pkg = "Kerberos" // KERB_S4U_LOGON: UPN (or name) and realm
		if strings.Contains(name, "@") {
			realm = ""
		}
	}

	s4uMu.Lock()
	defer s4uMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := enableTCB(); err != nil {
		return fmt.Errorf("pam: S4U logon: %w", err)
	}
	logonName, nb := newLsaString("ervisio")
	var h windows.Handle
	var mode uint32
	r, _, _ := procLsaRegisterLogonProc.Call(uintptr(unsafe.Pointer(&logonName)), uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(&mode)))
	runtime.KeepAlive(nb)
	if err := ntErr("LsaRegisterLogonProcess", r); err != nil {
		return err
	}
	defer procLsaDeregister.Call(uintptr(h))

	pkgName, pb := newLsaString(pkg)
	var pkgID uint32
	r, _, _ = procLsaLookupAuthPackage.Call(uintptr(h), uintptr(unsafe.Pointer(&pkgName)), uintptr(unsafe.Pointer(&pkgID)))
	runtime.KeepAlive(pb)
	if err := ntErr("LsaLookupAuthenticationPackage", r); err != nil {
		return err
	}

	// MSV1_0_S4U_LOGON / KERB_S4U_LOGON: MessageType, Flags, two
	// UNICODE_STRINGs, then their characters in the same buffer.
	upn16, err := windows.UTF16FromString(upn)
	if err != nil {
		return &Error{Kind: ErrAuth, Msg: "invalid user name"}
	}
	realm16, err := windows.UTF16FromString(realm)
	if err != nil {
		return &Error{Kind: ErrAuth, Msg: "invalid user name"}
	}
	upn16, realm16 = upn16[:len(upn16)-1], realm16[:len(realm16)-1] // without NUL
	hdr := 8 + 2*int(unsafe.Sizeof(windows.NTUnicodeString{}))
	hdr = (hdr + 7) &^ 7
	words := make([]uint64, (hdr+2*(len(upn16)+len(realm16))+7)/8+1)
	base := unsafe.Pointer(&words[0])
	*(*uint32)(base) = s4uMessageType
	*(*uint32)(unsafe.Add(base, 4)) = 0
	us := (*[2]windows.NTUnicodeString)(unsafe.Add(base, 8))
	chars := unsafe.Slice((*uint16)(unsafe.Add(base, hdr)), len(upn16)+len(realm16)+1)
	copy(chars, upn16)
	copy(chars[len(upn16):], realm16)
	us[0] = windows.NTUnicodeString{Length: uint16(2 * len(upn16)), MaximumLength: uint16(2 * len(upn16)), Buffer: &chars[0]}
	us[1] = windows.NTUnicodeString{Length: uint16(2 * len(realm16)), MaximumLength: uint16(2 * len(realm16)), Buffer: &chars[len(upn16)]}
	authLen := uint32(hdr + 2*(len(upn16)+len(realm16)))

	var src tokenSource
	copy(src.Name[:], "ervisio")
	if r, _, e := procAllocLUID.Call(uintptr(unsafe.Pointer(&src.ID))); r == 0 {
		return fmt.Errorf("pam: AllocateLocallyUniqueId: %w", e)
	}
	origin, ob := newLsaString("ervisio")
	var (
		profile    uintptr
		profileLen uint32
		logonID    windows.LUID
		tok        windows.Token
		quotas     [8]uint64 // QUOTA_LIMITS
		sub        int32
	)
	r, _, _ = procLsaLogonUser.Call(uintptr(h), uintptr(unsafe.Pointer(&origin)), logonNetwork, uintptr(pkgID),
		uintptr(base), uintptr(authLen), 0, uintptr(unsafe.Pointer(&src)),
		uintptr(unsafe.Pointer(&profile)), uintptr(unsafe.Pointer(&profileLen)),
		uintptr(unsafe.Pointer(&logonID)), uintptr(unsafe.Pointer(&tok)),
		uintptr(unsafe.Pointer(&quotas)), uintptr(unsafe.Pointer(&sub)))
	runtime.KeepAlive(words)
	runtime.KeepAlive(ob)
	if profile != 0 {
		procLsaFreeReturnBuffer.Call(profile)
	}
	if r != 0 {
		return mapLogonError(windows.NTStatus(r).Errno())
	}
	return keepToken(user, tok)
}
