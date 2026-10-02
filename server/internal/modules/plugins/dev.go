package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// DevFile, when set (dev only, --dev-state-dir), is where plugins.loadDev keeps
// its list instead of ~/.config/ervisio/plugins-dev.json, so tests and agents
// never touch the user's own file.
var DevFile string

// devFile holds the plugin folders the user loaded with plugins.loadDev.
func devFile() string {
	if DevFile != "" {
		return DevFile
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, brand.UserDataDir, "plugins-dev.json")
}

type devState struct {
	Folders []string `json:"folders"`
}

func readDev() devState {
	var s devState
	if f := devFile(); f != "" {
		if b, err := os.ReadFile(f); err == nil {
			_ = json.Unmarshal(b, &s)
		}
	}
	return s
}

func registeredDev() []string { return readDev().Folders }

func saveDev(s devState) error {
	f := devFile()
	if f == "" {
		return fmt.Errorf("no home directory to keep the list in")
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return writeFileAtomic(f, append(b, '\n'), 0o600)
}

// expandHome turns ~ and ~/x into absolute paths.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
