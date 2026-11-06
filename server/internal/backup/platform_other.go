//go:build !linux

package backup

import (
	"errors"
	"os"
)

func privateOwner(fi os.FileInfo) bool { return fi.Mode()&os.ModeSymlink == 0 }
func openProtected(p string, flags int, mode os.FileMode) (*os.File, error) {
	return os.OpenFile(p, flags, mode)
}
func protectedReadFlags() int { return os.O_RDONLY }
func openArchiveRead(root *os.Root, rel string, _ bool) (*os.File, error) {
	return root.OpenFile(rel, protectedReadFlags(), 0)
}
func availableBytes(string) (int64, error) {
	return 0, errors.New("Backup execution currently supports Linux.")
}
func lockFile(string) (func(), error) {
	return nil, errors.New("Backup execution currently supports Linux.")
}
