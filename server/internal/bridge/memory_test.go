package bridge

import (
	"math"
	"runtime/debug"
	"slices"
	"testing"
)

func TestMemoryLimitPolicy(t *testing.T) {
	for _, tc := range []struct {
		value string
		ram   uint64
		want  int64
	}{
		{"auto", 952 << 20, 96 << 20},
		{"auto", 2 << 30, 96 << 20},
		{"auto", 4 << 30, -1},
		{"auto", 0, -1},
		{"128MiB", 952 << 20, 128 << 20},
		{"1GiB", 952 << 20, 1 << 30},
		{"1024B", 0, 1024},
		{"off", 952 << 20, math.MaxInt64},
	} {
		got, err := memoryLimit(tc.value, tc.ram)
		if err != nil || got != tc.want {
			t.Errorf("memoryLimit(%q,%d) = %d,%v; want %d", tc.value, tc.ram, got, err, tc.want)
		}
	}
	for _, value := range []string{"", "-1MiB", "96MB", "128", "1.5GiB", "18446744073709551615TiB"} {
		if _, err := memoryLimit(value, 952<<20); err == nil {
			t.Errorf("accepted invalid memory limit %q", value)
		}
	}
}

func TestConfigureMemoryLimitPreservesExplicitRuntimePolicy(t *testing.T) {
	previous := debug.SetMemoryLimit(192 << 20)
	t.Cleanup(func() { debug.SetMemoryLimit(previous) })
	t.Setenv("GOMEMLIMIT", "192MiB")
	if err := ConfigureMemoryLimit("auto"); err != nil {
		t.Fatal(err)
	}
	if got := debug.SetMemoryLimit(-1); got != 192<<20 {
		t.Fatalf("overrode explicit GOMEMLIMIT: %d", got)
	}
	if err := ConfigureMemoryLimit("128MiB"); err != nil {
		t.Fatal(err)
	}
	if got := debug.SetMemoryLimit(-1); got != 128<<20 {
		t.Fatalf("explicit bridge flag not applied: %d", got)
	}
}

func TestBridgeMemoryOverrideSurvivesCleanEnvironmentAndAdminStart(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "128MiB")
	spec := &Spec{Config: "/etc/ervisio/ervisio.conf"}
	for _, admin := range []bool{false, true} {
		args := spec.bridgeArgs(admin)
		i := slices.Index(args, "--memory-limit")
		if i < 0 || args[i+1] != "128MiB" {
			t.Fatalf("operator override lost, admin=%v: %v", admin, args)
		}
	}
}
