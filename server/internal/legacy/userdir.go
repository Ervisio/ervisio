package legacy

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// MigrateUserDir copies ~/.config/linuxadmin (preferences, plugin settings
// such as the Docker plugin's registries) to ~/.config/ervisio, once: when
// the new folder holds no files yet. It runs in the user's own bridge, as
// the user. The old folder stays.
func MigrateUserDir(home string) (bool, error) {
	if runtime.GOOS == "windows" || !filepath.IsAbs(home) {
		return false, nil
	}
	oldD := filepath.Join(home, brand.LegacyUserDataDir)
	newD := filepath.Join(home, brand.UserDataDir)
	removeStale(newD)
	fi, err := os.Lstat(oldD)
	if err != nil || !fi.IsDir() {
		return false, nil // none, or a symlink: not ours to follow
	}
	if exists(newD) && (!isDir(newD) || hasFiles(newD)) {
		return false, nil
	}
	if err := installTree(oldD, newD, nil, nil); err != nil {
		return false, err
	}
	return true, nil
}
