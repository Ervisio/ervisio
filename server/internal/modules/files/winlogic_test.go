package files

import (
	"os"
	"testing"
)

func TestIsInsideWin(t *testing.T) {
	cases := []struct {
		c, p string
		want bool
	}{
		{`C:\a\b`, `C:\a`, true},
		{`c:\A\b`, `C:\a`, true},
		{`C:\a`, `C:\a`, true},
		{`C:\ab`, `C:\a`, false},
		{`C:/a/b`, `C:\a\`, true},
		{`C:\a`, `C:\`, true},
		{`D:\a`, `C:\`, false},
	}
	for _, c := range cases {
		if got := isInsideWin(c.c, c.p); got != c.want {
			t.Errorf("isInsideWin(%q,%q)=%v", c.c, c.p, got)
		}
	}
}

func TestWindowsProtectedPath(t *testing.T) {
	extra := []string{`C:\Windows`, `C:\Program Files`}
	for p, want := range map[string]bool{
		`C:\`: true, `c:`: true, `C:\windows`: true, `C:\Users`: true, `C:\Users\bob`: false,
		`D:\data`: false, `C:\Program Files`: true, `C:\Program Files\x`: false, `D:\Windows`: true,
	} {
		if got := windowsProtectedPath(p, extra); got != want {
			t.Errorf("windowsProtectedPath(%q)=%v", p, got)
		}
	}
}

func TestWinReadOnlyFromMode(t *testing.T) {
	for m, want := range map[os.FileMode]bool{0o644: false, 0o755: false, 0o444: true, 0o555: true} {
		got, err := winReadOnlyFromMode(m)
		if err != nil || got != want {
			t.Errorf("%o: %v %v", m, got, err)
		}
	}
	for _, m := range []os.FileMode{0o000, 0o200, 0o644 | os.ModeSetuid, 0o755 | os.ModeSticky} {
		if _, err := winReadOnlyFromMode(m); err == nil {
			t.Errorf("%o accepted", m)
		}
	}
}

func TestIsNameSurrogate(t *testing.T) {
	if !isNameSurrogate(0xA000000C) || !isNameSurrogate(0xA0000003) || isNameSurrogate(0x9000001A) {
		t.Error("tags")
	}
	if !isLinkMode(os.ModeSymlink) || !isLinkMode(os.ModeIrregular) || isLinkMode(0o644) {
		t.Error("modes")
	}
}
