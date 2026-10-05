//go:build windows

package pam

import (
	"errors"
	"syscall"
	"testing"
)

func TestSplitUser(t *testing.T) {
	cases := []struct {
		in, name, domain string
		ok               bool
	}{
		{"alice", "alice", ".", true},
		{`CORP\alice`, "alice", "CORP", true},
		{"alice@corp.example", "alice@corp.example", "", true},
		{"", "", ".", false},
		{`CORP\`, "", "CORP", false},
		{`\alice`, "alice", "", false},
		{"@corp", "@corp", "", false},
		{"alice@", "alice@", "", false},
	}
	for _, c := range cases {
		n, d, ok := splitUser(c.in)
		if ok != c.ok || (ok && (n != c.name || d != c.domain)) {
			t.Errorf("splitUser(%q) = %q,%q,%v", c.in, n, d, ok)
		}
	}
}

func TestInputValidation(t *testing.T) {
	for _, c := range [][2]string{{"", "x"}, {"a\x00b", "x"}, {"alice", "p\x00w"}, {`a\b\c`, "x"}} {
		if err := Authenticate("", c[0], c[1], ""); !errors.Is(err, ErrAuth) {
			t.Errorf("Authenticate(%q): %v", c[0], err)
		}
	}
	if err := CheckAccount("", "a\x00b", ""); !errors.Is(err, ErrAccount) {
		t.Errorf("CheckAccount NUL: %v", err)
	}
}

func TestMapLogonError(t *testing.T) {
	if !errors.Is(mapLogonError(errLogonFailure), ErrAuth) {
		t.Error("1326 should be ErrAuth")
	}
	for _, e := range []syscall.Errno{errAccountDisabled, errAccountExpired, errPasswordExpired, errAccountRestriction, errAccountLockedOut} {
		if !errors.Is(mapLogonError(e), ErrAccount) {
			t.Errorf("%d should be ErrAccount", e)
		}
	}
	if err := mapLogonError(syscall.Errno(5)); errors.Is(err, ErrAuth) || errors.Is(err, ErrAccount) {
		t.Error("other errors stay plain")
	}
}

func TestUserTokenMissing(t *testing.T) {
	if _, ok := UserToken("nobody-here"); ok {
		t.Error("unexpected token")
	}
}
