package sys

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
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

func TestRunOutput(t *testing.T) {
	ctx := context.Background()
	out, err := Output(ctx, "printf", "%s|%s", "a b", "$(id)")
	if err != nil || string(out) != "a b|$(id)" {
		t.Fatalf("%q %v", out, err)
	}
	err = Run(ctx, "sh", "-c", "echo boom >&2; exit 3")
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 3 || !strings.Contains(ee.Error(), "boom") {
		t.Fatalf("%v", err)
	}
	if err := Run(ctx, "definitely-not-a-command-xyz"); !rpc.IsCode(err, rpc.Unavailable) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := (Cmd{Name: "head", Args: []string{"-c", "100", "/dev/zero"}, MaxOutput: 10}).Output(ctx); err == nil {
		t.Fatal("expected overflow error")
	}
	if err := (Cmd{Name: "sleep", Args: []string{"5"}, Timeout: 100 * time.Millisecond}).Run(ctx); !rpc.IsCode(err, rpc.Unavailable) {
		t.Fatalf("timeout: %v", err)
	}
	if out, _ := (Cmd{Name: "sh", Args: []string{"-c", "echo $PATH $FOO"}, Env: []string{"FOO=bar"}}).Output(ctx); strings.TrimSpace(string(out)) != SafePath+" bar" {
		t.Fatalf("env: %q", out)
	}
}

func TestStream(t *testing.T) {
	var lines []string
	err := Stream(context.Background(), "printf", []string{"a\nb\nc\n"}, func(l string) error {
		lines = append(lines, l)
		return nil
	})
	if err != nil || strings.Join(lines, ",") != "a,b,c" {
		t.Fatalf("%v %v", lines, err)
	}
	stop := errors.New("stop")
	n := 0
	err = Stream(context.Background(), "yes", nil, func(string) error {
		n++
		if n == 5 {
			return stop
		}
		return nil
	})
	if err != stop {
		t.Fatalf("%v", err)
	}
}
