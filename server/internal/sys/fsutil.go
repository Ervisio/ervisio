package sys

import (
	"crypto/rand"
	"encoding/hex"
	"os"
)

// RandHex returns 2n random hex characters.
func RandHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// SyncDir flushes a directory so a rename inside it survives a crash.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	_ = d.Sync()
	return nil
}
