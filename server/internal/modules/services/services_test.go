package services

import (
	"os"
	"path/filepath"
	"reflect"
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
		"libvirtd.service": PurposeContainers, "sshd.service": PurposeSystem, "NetworkManager.service": PurposeSystem, "linuxadmin.service": PurposeWeb,
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

func TestParseShow(t *testing.T) {
	out := []byte("Id=a.service\nMemoryCurrent=1409024\nCPUUsageNSec=[not set]\nMainPID=42\nStateChangeTimestamp=@1790871752\nListen=/run/x (Stream)\nListen=[::]:22 (Stream)\n\nId=b.service\nMemoryCurrent=18446744073709551615\n")
	bl := parseShow(out)
	if len(bl) != 2 || bl[0]["Id"] != "a.service" || bl[1]["Id"] != "b.service" {
		t.Fatalf("blocks %v", bl)
	}
	if bl[0]["Listen"] != "/run/x (Stream)\n[::]:22 (Stream)" {
		t.Errorf("repeated keys: %q", bl[0]["Listen"])
	}
	u := &Unit{Name: "a.socket", Active: "active"}
	applyShow(u, bl[0])
	if u.Memory == nil || *u.Memory != 1409024 || u.CPUNs != nil || u.PID != 42 || u.Since != 1790871752 || len(u.Listen) != 2 {
		t.Errorf("unit %+v", u)
	}
	u2 := &Unit{Name: "b.service"}
	applyShow(u2, bl[1])
	if u2.Memory != nil {
		t.Errorf("all-ones memory must be nil")
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

func TestDiffSnapshots(t *testing.T) {
	a := map[string]listedUnit{"a.service": {Name: "a.service", Active: "active", Sub: "running"}, "b.service": {Name: "b.service", Active: "active", Sub: "running"}}
	b := map[string]listedUnit{"a.service": {Name: "a.service", Active: "failed", Sub: "failed"}, "c.service": {Name: "c.service", Active: "active", Sub: "running"}}
	ch, rm := diffSnapshots(a, b)
	if !reflect.DeepEqual(ch, []string{"a.service", "c.service"}) || !reflect.DeepEqual(rm, []string{"b.service"}) {
		t.Fatalf("%v %v", ch, rm)
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

func TestOverridePath(t *testing.T) {
	old := overrideRoot
	overrideRoot = t.TempDir()
	defer func() { overrideRoot = old }()
	want := filepath.Join(overrideRoot, "nginx.service.d", "override.conf")
	if overridePath("nginx.service") != want {
		t.Fatal(overridePath("nginx.service"))
	}
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(want, []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(want); string(b) != "[Service]\n" {
		t.Fatal(string(b))
	}
}
