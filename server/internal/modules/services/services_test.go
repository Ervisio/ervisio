package services

import (
	"errors"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"testing"
)

func TestValidUnitName(t *testing.T) {
	good := []string{"nginx.service", "getty@tty1.service", "foo@.service", "sys-devices-x.device", `dev-disk-by\x2dlabel.device`, "systemd-tmpfiles-clean.timer", "dbus.socket", "a:b.service"}
	bad := []string{"", "nginx", "-x.service", ".hidden.service", "../x.service", "a/b.service", "a b.service", "x.service;rm", "x.service\n", "x.exe", ".service", "$(id).service"}
	for _, n := range good {
		if !ValidUnitName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range bad {
		if ValidUnitName(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
}

func TestPurpose(t *testing.T) {
	cases := map[string]string{
		"nginx.service": PurposeWeb, "php-fpm.service": PurposeWeb, "php8.2-fpm.service": PurposeWeb, "postgresql@15-main.service": PurposeWeb, "mariadb.service": PurposeWeb,
		"redis.service": PurposeWeb, "docker.service": PurposeContainers, "docker.socket": PurposeContainers, "containerd.service": PurposeContainers,
		"libvirtd.service": PurposeContainers, "sshd.service": PurposeSystem, "NetworkManager.service": PurposeSystem, "ervisio.service": PurposeWeb,
	}
	for n, want := range cases {
		if got := PurposeOf(n); got != want {
			t.Errorf("PurposeOf(%s) = %s, want %s", n, got, want)
		}
	}
}

func TestParseListUnitsJSON(t *testing.T) {
	out := []byte(`[{"unit":"sshd.service","load":"loaded","active":"active","sub":"running","description":"OpenSSH Daemon"},{"unit":"x.service","load":"not-found","active":"inactive","sub":"dead","description":"x.service"}]`)
	us, err := parseListUnits(out)
	if err != nil || len(us) != 2 || us[0].Name != "sshd.service" || us[0].Sub != "running" || us[0].Description != "OpenSSH Daemon" {
		t.Fatalf("got %+v %v", us, err)
	}
}

func TestParseListUnitsPlain(t *testing.T) {
	out := []byte("  UNIT          LOAD   ACTIVE SUB     DESCRIPTION\n  sshd.service  loaded active running OpenSSH Daemon\n● foo.service   loaded failed failed  Foo thing\n")
	us, _ := parseListUnits(out)
	if len(us) != 2 || us[1].Name != "foo.service" || us[1].Active != "failed" || us[1].Description != "Foo thing" {
		t.Fatalf("got %+v", us)
	}
}

func TestParseUnitFiles(t *testing.T) {
	m, err := parseUnitFiles([]byte(`[{"unit_file":"a.service","state":"enabled","preset":"enabled"},{"unit_file":"b.service","state":"masked","preset":null}]`))
	if err != nil || m["a.service"] != "enabled" || m["b.service"] != "masked" {
		t.Fatalf("got %v %v", m, err)
	}
	m, _ = parseUnitFiles([]byte("UNIT FILE STATE PRESET\na.service enabled enabled\n\n2 unit files listed.\n"))
	if m["a.service"] != "enabled" || len(m) != 1 {
		t.Fatalf("plain: %v", m)
	}
}

func TestStateOf(t *testing.T) {
	cases := [][3]string{{"active", "running", StateRunning}, {"active", "exited", StateFinished}, {"failed", "failed", StateFailed}, {"inactive", "dead", StateStopped}, {"activating", "start", StateRunning}}
	for _, c := range cases {
		if got := stateOf(c[0], c[1]); got != c[2] {
			t.Errorf("%v -> %s", c, got)
		}
	}
}

func TestHints(t *testing.T) {
	h := hintFor(hintInput{Name: "systemd-networkd-wait-online.service", Active: map[string]bool{"NetworkManager.service": true}})
	if h == nil || h.ID != "networkd-wait-online" {
		t.Fatalf("got %v", h)
	}
	if hintFor(hintInput{Name: "systemd-networkd-wait-online.service"}) != nil {
		t.Error("no hint without NetworkManager")
	}
	if h := hintFor(hintInput{Name: "x.service", Result: "exit-code", ExitCode: 203}); h == nil || h.ID != "exec-missing" {
		t.Errorf("203: %v", h)
	}
	if h := hintFor(hintInput{Name: "x.service", Result: "start-limit-hit"}); h == nil || h.ID != "start-limit" {
		t.Errorf("limit: %v", h)
	}
}

func TestParseJournal(t *testing.T) {
	out := []byte(`{"__REALTIME_TIMESTAMP":"1790871752123456","PRIORITY":"3","MESSAGE":"boom"}` + "\n" +
		`{"__REALTIME_TIMESTAMP":"1790871753000000","MESSAGE":[104,105]}` + "\nnot json\n")
	l := parseJournal(out)
	if len(l) != 2 || l[0].Time != 1790871752123 || l[0].Priority != 3 || l[0].Message != "boom" || l[1].Message != "hi" || l[1].Priority != 6 {
		t.Fatalf("%+v", l)
	}
}

func TestWinState(t *testing.T) {
	cases := []struct {
		state, exit      uint32
		active, sub, sim string
	}{
		{scmRunning, 0, "active", "running", StateRunning},
		{scmStartPending, 0, "activating", "start-pending", StateRunning},
		{scmPaused, 0, "active", "paused", StateRunning},
		{scmStopPending, 0, "deactivating", "stop-pending", StateStopped},
		{scmStopped, 0, "inactive", "dead", StateStopped},
		{scmStopped, errServiceNeverStarted, "inactive", "dead", StateStopped},
		{scmStopped, 1, "failed", "failed", StateFailed},
		{99, 0, "inactive", "dead", StateStopped},
	}
	for _, c := range cases {
		a, s, st := winState(c.state, c.exit)
		if a != c.active || s != c.sub || st != c.sim {
			t.Errorf("winState(%d,%d) = %s %s %s", c.state, c.exit, a, s, st)
		}
	}
}

func TestWinEnabled(t *testing.T) {
	cases := []struct {
		start   uint32
		delayed bool
		enabled string
		name    string
	}{
		{scmAutoStart, false, "enabled", "auto"}, {scmAutoStart, true, "enabled", "delayed"},
		{scmDemandStart, false, "disabled", "manual"}, {scmDisabled, false, "masked", "disabled"},
		{scmBootStart, false, "static", "boot"}, {scmSystemStart, false, "static", "system"}, {42, false, "", ""},
	}
	for _, c := range cases {
		if g := winEnabled(c.start, c.delayed); g != c.enabled {
			t.Errorf("winEnabled(%d) = %q", c.start, g)
		}
		if g := winStartTypeName(c.start, c.delayed); g != c.name {
			t.Errorf("winStartTypeName(%d,%v) = %q", c.start, c.delayed, g)
		}
	}
	for a, want := range map[string]uint32{"enable": scmAutoStart, "disable": scmDemandStart, "unmask": scmDemandStart, "mask": scmDisabled} {
		if g, ok := winActionStartType(a); !ok || g != want {
			t.Errorf("action %s = %d,%v", a, g, ok)
		}
	}
	if _, ok := winActionStartType("start"); ok {
		t.Error("start is not a start-type action")
	}
}

func TestValidServiceName(t *testing.T) {
	for _, n := range []string{"W32Time", "Windows Update", "Spooler", "wuauserv", "a.b-c"} {
		if !validServiceName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range []string{"", " x", "a/b", `a\b`, "a\nb", string(make([]byte, 300))} {
		if validServiceName(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
}

func TestWinError(t *testing.T) {
	code := func(e uint32) rpc.Code {
		var re *rpc.Error
		if !errors.As(winError(e, "x", "start"), &re) {
			t.Fatal("not an rpc error")
		}
		return re.Code
	}
	if code(errAccessDenied) != rpc.NeedsAdmin || code(errServiceDoesNotExist) != rpc.NotFound ||
		code(errServiceAlreadyRun) != rpc.Conflict || code(errServiceRequestTmout) != rpc.Unavailable || code(9999) != rpc.Conflict {
		t.Fatal("unexpected code mapping")
	}
}
