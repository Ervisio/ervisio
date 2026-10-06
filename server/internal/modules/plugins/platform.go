package plugins

import (
	"fmt"
	"runtime"
)

// Platforms: the operating systems a plugin runs on, "linux" and/or
// "windows" (manifest and catalog field "platforms"). A plugin that does not
// say is a Linux plugin: every plugin written before Windows support was.
// An incompatible plugin is shown with its platforms but cannot be
// installed, enabled or run, like one that needs a newer core.

var knownPlatforms = map[string]bool{"linux": true, "windows": true}

// effectivePlatforms is the declared list, or ["linux"].
func effectivePlatforms(declared []string) []string {
	if len(declared) == 0 {
		return []string{"linux"}
	}
	return declared
}

func validatePlatforms(p []string) error {
	seen := map[string]bool{}
	for _, s := range p {
		if !knownPlatforms[s] {
			return fmt.Errorf("platforms: %q is not linux or windows", s)
		}
		if seen[s] {
			return fmt.Errorf("platforms: %q is listed twice", s)
		}
		seen[s] = true
	}
	return nil
}

// hostPlatform is "linux" or "windows" (other unix systems count as linux).
func hostPlatform() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	return "linux"
}

// platformProblem says why a plugin for these platforms cannot run here, or "".
func platformProblem(name string, declared []string) string {
	for _, p := range effectivePlatforms(declared) {
		if p == hostPlatform() {
			return ""
		}
	}
	if hostPlatform() == "windows" {
		return fmt.Sprintf("%s works only on Linux.", name)
	}
	return fmt.Sprintf("%s works only on Windows.", name)
}
