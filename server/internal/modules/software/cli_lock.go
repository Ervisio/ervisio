package software

import (
	"fmt"
	"os"
	"path/filepath"
)

func lockCLI(root string) (func(), error) {
	path := filepath.Join(root, "install.lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|oNoFollow|oNonblock, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("Unsafe CLI lock file")
	}
	if err := fileTrust(path, info, os.Geteuid()); err != nil {
		f.Close()
		return nil, err
	}
	if err := lockCLIFile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("Another CLI installation is running for this account")
	}
	return func() { unlockCLIFile(f); _ = f.Close() }, nil
}
