package backup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var errBusy = errors.New("Another backup operation is running for this account.")

func waitRunLock(ctx context.Context, p string) (func(), error) {
	for {
		unlock, err := lockFile(p)
		if !errors.Is(err, errBusy) {
			return unlock, err
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

type spoolMarker struct {
	JobID string `json:"jobId"`
	Name  string `json:"name"`
	MAC   string `json:"mac"`
}

func spoolMAC(key []byte, job, name string) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("ervisio-private-spool-v1\n" + job + "\n" + name))
	return hex.EncodeToString(h.Sum(nil))
}

func markSpool(p string, c *Config) error {
	name := filepath.Base(p)
	marker := spoolMarker{JobID: c.ID, Name: name, MAC: spoolMAC(c.ownerKey, c.ID, name)}
	b, _ := json.Marshal(marker)
	return atomicPrivate(filepath.Join(p, ".ervisio-owner"), b)
}

// A killed worker may not run defer. Only directories carrying this job's
// authenticated marker are removed, while the account-wide run lock is held.
func cleanOwnedSpools(s *store, c *Config) error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "spool-"+c.ID+"-") {
			continue
		}
		p := filepath.Join(s.dir, entry.Name())
		fi, err := os.Lstat(p)
		if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || !privateOwner(fi) {
			continue
		}
		b, err := readPrivate(filepath.Join(p, ".ervisio-owner"), 2048)
		if err != nil {
			continue
		}
		var marker spoolMarker
		if decodeStrict(b, &marker) != nil || marker.JobID != c.ID || marker.Name != entry.Name() {
			continue
		}
		expected, err := hex.DecodeString(spoolMAC(c.ownerKey, c.ID, entry.Name()))
		if err != nil {
			continue
		}
		actual, err := hex.DecodeString(marker.MAC)
		if err != nil || !hmac.Equal(expected, actual) {
			continue
		}
		if err = os.RemoveAll(p); err != nil {
			return errors.New("An authenticated stale backup spool could not be cleaned safely.")
		}
	}
	return nil
}
