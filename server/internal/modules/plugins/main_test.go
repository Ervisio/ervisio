package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points platform home/config variables at a throwaway directory so the
// tests never read or write the developer's real ~/.config/ervisio files
// (plugins-dev.json would otherwise add the developer's folders to List).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "plugins-test-home-")
	if err != nil {
		panic(err)
	}
	for key, value := range testProfileEnv(dir) {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func testProfileEnv(dir string) map[string]string {
	return map[string]string{
		"HOME":            dir,
		"USERPROFILE":     dir,
		"XDG_CONFIG_HOME": filepath.Join(dir, ".config"),
		"APPDATA":         filepath.Join(dir, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(dir, "AppData", "Local"),
	}
}

// Tests that replace a home directory must replace Windows' home/config too.
func setTestProfile(t *testing.T, dir string) {
	t.Helper()
	for key, value := range testProfileEnv(dir) {
		t.Setenv(key, value)
	}
}
