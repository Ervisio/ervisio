//go:build unix

package services

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

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

func TestDiffSnapshots(t *testing.T) {
	a := map[string]listedUnit{"a.service": {Name: "a.service", Active: "active", Sub: "running"}, "b.service": {Name: "b.service", Active: "active", Sub: "running"}}
	b := map[string]listedUnit{"a.service": {Name: "a.service", Active: "failed", Sub: "failed"}, "c.service": {Name: "c.service", Active: "active", Sub: "running"}}
	ch, rm := diffSnapshots(a, b)
	if !reflect.DeepEqual(ch, []string{"a.service", "c.service"}) || !reflect.DeepEqual(rm, []string{"b.service"}) {
		t.Fatalf("%v %v", ch, rm)
	}
}
