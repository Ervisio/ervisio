package plugins

import (
	"os"
	"testing"
)

// TestMain points HOME and XDG_CONFIG_HOME at a throwaway directory so the
// tests never read or write the developer's real ~/.config/ervisio files
// (plugins-dev.json would otherwise add the developer's folders to List).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "plugins-test-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", dir)
	os.Setenv("XDG_CONFIG_HOME", dir+"/.config")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
