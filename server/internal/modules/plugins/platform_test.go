package plugins

import (
	"runtime"
	"testing"
)

func TestPlatforms(t *testing.T) {
	if err := validatePlatforms([]string{"linux", "windows"}); err != nil {
		t.Error(err)
	}
	for _, bad := range [][]string{{"macos"}, {"linux", "linux"}} {
		if validatePlatforms(bad) == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	other := "windows"
	if runtime.GOOS == "windows" {
		other = "linux"
	}
	if platformProblem("P", []string{"linux", "windows"}) != "" || platformProblem("P", []string{hostPlatform()}) != "" {
		t.Error("compatible plugin refused")
	}
	if platformProblem("P", []string{other}) == "" {
		t.Error("plugin for the other OS accepted")
	}
	if got := effectivePlatforms(nil); len(got) != 1 || got[0] != "linux" {
		t.Errorf("default %v", got)
	}
}
