package overview

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func TestFailedAlerts(t *testing.T) {
	js := []byte(`[{"unit":"systemd-networkd-wait-online.service","load":"loaded","active":"failed","sub":"failed","description":"Wait for Network to be Configured"},
{"unit":"foo.service","load":"loaded","active":"failed","sub":"failed","description":"Foo"}]`)
	a := failedAlerts(js, nil)
	if len(a) != 2 || a[0].Severity != "err" || a[0].Action.Section != "services" || a[0].Action.Params["unit"] != "systemd-networkd-wait-online.service" || a[0].Detail != "Wait for Network to be Configured" {
		t.Fatalf("%+v", a)
	}
	if len(failedAlerts([]byte(`[]`), nil)) != 0 || len(failedAlerts([]byte(``), nil)) != 0 || len(failedAlerts([]byte(`nope`), nil)) != 0 {
		t.Fatal("empty input")
	}
	var many []string
	for i := 0; i < 9; i++ {
		many = append(many, `{"unit":"u`+string(rune('a'+i))+`.service","description":"x"}`)
	}
	m := failedAlerts([]byte("["+strings.Join(many, ",")+"]"), nil)
	if len(m) != maxFailed+1 || m[maxFailed].ID != "units-more" || m[maxFailed].Vars["count"] != 3 {
		t.Fatalf("%+v", m)
	}
}

func TestDescribeFailure(t *testing.T) {
	got := describeFailure("ActiveEnterTimestamp=\nInactiveEnterTimestamp=Wed 2026-10-01 14:35:02 CEST\nResult=exit-code\nExecMainStatus=1\n")
	if got != "Failed Wed 2026-10-01 14:35:02 CEST (exit-code)" {
		t.Fatal(got)
	}
}

func TestUpdateParsers(t *testing.T) {
	arch := parseArchUpdates([]byte("linux 7.2.6.arch2-1 -> 7.2.7.arch1-1\nfirefox 140.0-1 -> 140.0.1-1\n"))
	if len(arch) != 2 || arch[0] != "linux 7.2.7.arch1-1" {
		t.Fatalf("%v", arch)
	}
	apt := parseAptUpdates([]byte("Listing... Done\nbash/jammy-updates 5.1-6ubuntu1.1 amd64 [upgradable from: 5.1-6ubuntu1]\ncurl/jammy-security 7.81.0-1ubuntu1.18 amd64 [upgradable from: 7.81.0-1ubuntu1.16]\n"))
	if len(apt) != 2 || apt[1] != "curl 7.81.0-1ubuntu1.18" {
		t.Fatalf("%v", apt)
	}
	dnf := parseDnfUpdates([]byte("\nkernel.x86_64   6.5.6-300.fc39   updates\nbash.x86_64   5.2.21-1.fc39   updates\n\nObsoleting Packages\nfoo.x86_64 1 2\n"))
	if len(dnf) != 2 || dnf[0] != "kernel 6.5.6-300.fc39" {
		t.Fatalf("%v", dnf)
	}
	if a := updatesFrom(nil); a[0].Severity != "ok" {
		t.Fatalf("%+v", a)
	}
	if a := updatesFrom(arch); a[0].Severity != "warn" || a[0].Vars["count"] != 2 || !strings.Contains(a[0].Detail, "linux") {
		t.Fatalf("%+v", a)
	}
}

func TestSSH(t *testing.T) {
	n := countSSHFailures([]byte("Accepted password for bob from 1.2.3.4 port 1 ssh2\nFailed password for root from 1.2.3.4 port 5 ssh2\nInvalid user admin from 5.6.7.8 port 9\nFailed password for invalid user x from 1.1.1.1 port 3 ssh2\n"))
	if n != 3 {
		t.Fatal(n)
	}
}

func TestMountsAndDisk(t *testing.T) {
	m := parseMounts(strings.NewReader("/dev/nvme0n1p2 / btrfs rw 0 0\n/dev/nvme0n1p2 /home btrfs rw 0 0\ntmpfs /run tmpfs rw 0 0\n/dev/sda1 /mnt/my\\040disk ext4 rw 0 0\n"))
	if len(m) != 2 || m[0].point != "/" || m[1].point != "/mnt/my disk" {
		t.Fatalf("%+v", m)
	}
	if _, ok := diskAlert("/", 80, 1); ok {
		t.Fatal("80% is fine")
	}
	if a, ok := diskAlert("/", 90, 1<<30); !ok || a.Severity != "warn" {
		t.Fatalf("%+v", a)
	}
	if a, _ := diskAlert("/", 97, 1<<30); a.Severity != "err" {
		t.Fatalf("%+v", a)
	}
	if len(swapFrom(0, 0)) != 0 || len(swapFrom(1000, 600)) != 0 || len(swapFrom(1000, 400)) != 1 {
		t.Fatal("swap thresholds")
	}
}

func TestCollectLive(t *testing.T) {
	a := newAlerter().collect(context.Background())
	if a == nil {
		t.Fatal("nil slice")
	}
	for i := 1; i < len(a); i++ {
		if severityRank[a[i-1].Severity] > severityRank[a[i].Severity] {
			t.Fatalf("not sorted: %+v", a)
		}
	}
}

func TestValidateArgv(t *testing.T) {
	for _, bad := range [][]string{nil, {}, {""}, {"echo", "a\x00b"}, make([]string, maxArgs+1)} {
		if validateArgv(bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if validateArgv([]string{"echo", "", "x"}) != nil {
		t.Error("empty later args are fine")
	}
}

func TestRunArgv(t *testing.T) {
	ctx := context.Background()
	r, err := runArgv(ctx, []string{"sh", "-c", "echo out; echo err >&2; exit 3"}, 5*time.Second, "")
	if err != nil || r.OK || r.ExitCode != 3 || !strings.Contains(r.Output, "out") || !strings.Contains(r.Output, "err") {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = runArgv(ctx, []string{"true"}, 5*time.Second, "")
	if err != nil || !r.OK {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = runArgv(ctx, []string{"sleep", "5"}, 200*time.Millisecond, "")
	if err != nil || !r.TimedOut || r.ExitCode != -1 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err = runArgv(ctx, []string{"definitely-not-a-command"}, time.Second, ""); !rpc.IsCode(err, rpc.Unavailable) {
		t.Fatalf("%v", err)
	}
	r, _ = runArgv(ctx, []string{"sh", "-c", "head -c 400000 /dev/zero | tr '\\0' a"}, 5*time.Second, "")
	if !r.Truncated || len(r.Output) != maxActionOutput {
		t.Fatalf("truncated=%v len=%d", r.Truncated, len(r.Output))
	}
}

func TestProcStat(t *testing.T) {
	ps, ok := parseProcStat([]byte("1234 (Web Content (x)) S 1 1234 1234 0 -1 4194560 100 0 0 0 150 50 0 0 20 0 5 0 100 1000000 777 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 3 0 0 0 0 0\n"))
	if !ok || ps.pid != 1234 || ps.name != "Web Content (x)" || ps.state != "S" || ps.ticks != 200 || ps.rss != 777 {
		t.Fatalf("%+v", ps)
	}
	if _, ok := parseProcStat([]byte("garbage")); ok {
		t.Fatal("garbage parsed")
	}
	l := rankProcesses([]Process{{PID: 1, CPU: 1, Memory: 9}, {PID: 2, CPU: 5, Memory: 1}, {PID: 3, CPU: 3, Memory: 5}}, "cpu", 2)
	if len(l) != 2 || l[0].PID != 2 || l[1].PID != 3 {
		t.Fatalf("%+v", l)
	}
	if l = rankProcesses([]Process{{PID: 1, Memory: 9}, {PID: 2, Memory: 10}}, "mem", 5); l[0].PID != 2 {
		t.Fatalf("%+v", l)
	}
}

func TestLiveProcesses(t *testing.T) {
	res, err := processes(context.Background(), &rpc.Call{Params: []byte(`{"sort":"mem","limit":3}`)})
	if err != nil {
		t.Fatal(err)
	}
	l := res.([]Process)
	if len(l) != 3 || l[0].Memory == 0 || l[0].Command == "" {
		t.Fatalf("%+v", l)
	}
	if _, err := processes(context.Background(), &rpc.Call{Params: []byte(`{"sort":"bogus"}`)}); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatal(err)
	}
}

func TestUnitShowAndTail(t *testing.T) {
	s := parseUnitShow("nginx.service", "Description=A web server\nLoadState=loaded\nActiveState=active\nSubState=running\nUnitFileState=enabled\n")
	if s.Active != "active" || s.Sub != "running" || s.Description != "A web server" || s.Enabled != "enabled" {
		t.Fatalf("%+v", s)
	}
	if s := parseUnitShow("x.service", "LoadState=not-found\nActiveState=inactive\n"); s.Active != "not-found" {
		t.Fatalf("%+v", s)
	}
	if u, ok := normalizeUnit("nginx"); !ok || u != "nginx.service" {
		t.Fatal(u)
	}
	for _, bad := range []string{"", "-x", "a b", "a;b", "../x"} {
		if _, ok := normalizeUnit(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	f := t.TempDir() + "/x.log"
	if err := os.WriteFile(f, []byte("a\nb\nc\nd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := tailFile(f, 2)
	if err != nil || strings.Join(r.(map[string]any)["lines"].([]string), ",") != "c,d" {
		t.Fatalf("%v %v", r, err)
	}
	if _, err := tailFile("relative.log", 2); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatal(err)
	}
	if _, err := tailFile(t.TempDir(), 2); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatal(err)
	}
}
