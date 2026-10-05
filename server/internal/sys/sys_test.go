package sys

import (
	"strings"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	o := ParseOSRelease(strings.NewReader(`# comment
NAME="Arch Linux"
PRETTY_NAME="Arch \"Linux\""
ID=arch
ID_LIKE='archlinux other'
ANSI_COLOR="38;2;23;147;209"
LOGO=archlinux-logo
`))
	if o.ID != "arch" || o.Name != "Arch Linux" || o.PrettyName != `Arch "Linux"` || o.Logo != "archlinux-logo" || len(o.IDLike) != 2 {
		t.Fatalf("%+v", o)
	}
	empty := ParseOSRelease(strings.NewReader(""))
	if empty.ID != "linux" || empty.PrettyName != "Linux" {
		t.Fatalf("%+v", empty)
	}
}

func TestDistroColor(t *testing.T) {
	for id, want := range map[string]string{"arch": "#1793D1", "opensuse-tumbleweed": "#73BA25", "Ubuntu": "#E95420", "gentoo": DefaultDistroColor} {
		if got := DistroColor(id); got != want {
			t.Errorf("%s: %s", id, got)
		}
	}
}

func TestFindLogoRejectsPaths(t *testing.T) {
	for _, n := range []string{"../etc/passwd", "/etc/passwd", ".hidden", ""} {
		if FindLogo(n) != "" {
			t.Errorf("%q accepted", n)
		}
	}
}
