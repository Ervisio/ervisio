package users

import (
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func TestNativeUserMapping(t *testing.T) {
	now := time.Unix(1700000000, 0)
	u := nativeUser{Name: "mario", FullName: "Mario", Comment: "c", Flags: 0, PasswordAge: 86400, LastLogon: 5, AcctExpires: netForever}.
		toWinUser("S-1-5-21-1-2-3-1001", `C:\Users\mario`, now)
	if !u.Enabled || !u.PasswordRequire || u.PasswordLastSet != 1700000000-86400 || u.AccountExpires != 0 ||
		u.LastLogon != 5 || u.Description != "c" || u.Profile != `C:\Users\mario` {
		t.Errorf("%+v", u)
	}
	d := nativeUser{Flags: ufAccountDisable | ufPasswdNotReqd, AcctExpires: 100}.toWinUser("S", "", now)
	if d.Enabled || d.PasswordRequire || d.PasswordLastSet != 0 || d.AccountExpires != 100 {
		t.Errorf("%+v", d)
	}
}

func TestNetStatusError(t *testing.T) {
	if !rpc.IsCode(netStatusError("x", 5, ""), rpc.NeedsAdmin) {
		t.Error("5 should be needs_admin")
	}
	if rpc.IsCode(netStatusError("x", 2221, "no user"), rpc.NeedsAdmin) {
		t.Error("2221 must not be needs_admin")
	}
}

func TestClassifyPSErrorUnstructured(t *testing.T) {
	err := classifyPSError("x", "The module could not be loaded")
	if rpc.IsCode(err, rpc.NeedsAdmin) {
		t.Error("unstructured non-denied error mapped to needs_admin")
	}
	if err.Error() == "" || !contains(err.Error(), "module could not be loaded") {
		t.Errorf("stderr missing: %v", err)
	}
	if !rpc.IsCode(classifyPSError("x", "Access is denied."), rpc.NeedsAdmin) {
		t.Error("access denied must map to needs_admin")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
