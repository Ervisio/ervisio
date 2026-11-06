package backup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/modules/remote"
)

type manifest struct {
	archiveInfo
	Archive string `json:"archive"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
	MAC     string `json:"mac"`
}

func (m manifest) mac(key []byte) string {
	m.MAC = ""
	b, _ := json.Marshal(m)
	h := hmac.New(sha256.New, key)
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func (m manifest) authentic(key []byte) bool {
	expected, err := hex.DecodeString(m.mac(key))
	if err != nil {
		return false
	}
	actual, err := hex.DecodeString(m.MAC)
	return err == nil && hmac.Equal(expected, actual)
}

func readRemote(ctx context.Context, f remote.FS, p string, limit int64) ([]byte, error) {
	e, err := f.Stat(ctx, p)
	if err != nil {
		return nil, err
	}
	if e.Type != "file" || e.Size < 0 || e.Size > limit {
		return nil, errors.New("The backup metadata is not a bounded regular file.")
	}
	r, err := f.Open(ctx, p)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("Oversized remote backup metadata.")
	}
	return b, nil
}

func remoteDirectory(ctx context.Context, f remote.FS, p string) error {
	current := "/"
	for _, name := range strings.Split(strings.Trim(p, "/"), "/") {
		if name == "" {
			continue
		}
		current = path.Join(current, name)
		e, err := f.Stat(ctx, current)
		if errors.Is(err, fs.ErrNotExist) {
			if err = f.Mkdir(ctx, current); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			e, err = f.Stat(ctx, current)
		}
		if err != nil {
			return err
		}
		if e.Type != "dir" {
			return errors.New("Every destination component must be a real directory, not a link.")
		}
	}
	return nil
}

func ownDestination(ctx context.Context, f remote.FS, c *Config) (string, error) {
	dir := path.Join(c.Destination.Path, "ervisio-backup-"+c.ID)
	if err := remoteDirectory(ctx, f, dir); err != nil {
		return "", err
	}
	digest := sha256.Sum256(c.ownerKey)
	marker := struct {
		Schema string `json:"schema"`
		JobID  string `json:"jobId"`
		KeyID  string `json:"keyId"`
	}{"ervisio-backup-owner-v1", c.ID, hex.EncodeToString(digest[:])}
	expected, _ := json.Marshal(marker)
	markerPath := path.Join(dir, ".ervisio-backup.json")
	actual, err := readRemote(ctx, f, markerPath, 4096)
	if errors.Is(err, fs.ErrNotExist) {
		if err = uploadAtomic(ctx, f, markerPath, bytes.NewReader(expected), int64(len(expected))); err != nil {
			return "", err
		}
		actual, err = readRemote(ctx, f, markerPath, 4096)
	}
	if err != nil {
		return "", err
	}
	if !bytes.Equal(actual, expected) {
		return "", errors.New("This destination belongs to a different backup plan or its ownership marker changed. Choose another folder; nothing was deleted.")
	}
	if err = remote.SecureBackupDirectory(ctx, f, dir); err != nil {
		return "", errors.New("The private SFTP backup directory permissions could not be applied.")
	}
	return dir, nil
}

func uploadAtomic(ctx context.Context, f remote.FS, target string, r io.Reader, size int64) error {
	if _, err := f.Stat(ctx, target); err == nil {
		return errors.New("A backup target already exists and will not be overwritten.")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	tmp := path.Join(path.Dir(target), ".ervisio-upload-"+newID()+".part")
	journal, _ := ctx.Value(journalKey{}).(*uploadJournal)
	if journal != nil {
		if err := journal.track(tmp, size); err != nil {
			return err
		}
	}
	w, err := f.Create(ctx, tmp, size)
	if err != nil {
		if journal != nil && errors.Is(err, fs.ErrExist) {
			_ = journal.clear(tmp)
		}
		return err
	}
	cleanup := func() {
		if f.Remove(ctx, tmp, false) == nil && journal != nil {
			_ = journal.clear(tmp)
		}
	}
	_, err = copyBounded(ctx, w, r, size)
	if e := w.Close(); err == nil {
		err = e
	}
	if err != nil {
		cleanup()
		return err
	}
	if _, err = f.Stat(ctx, target); err == nil {
		cleanup()
		return errors.New("The backup target appeared during upload and was not overwritten.")
	} else if !errors.Is(err, fs.ErrNotExist) {
		cleanup()
		return err
	}
	// SFTP's standard Rename refuses an existing target; SMB also uses its
	// non-replacing Rename. Random run IDs additionally avoid normal collisions.
	if err = f.Rename(ctx, tmp, target); err != nil {
		cleanup()
		return err
	}
	if journal != nil {
		if err = journal.clear(tmp); err != nil {
			return err
		}
	}
	e, err := f.Stat(ctx, target)
	if err != nil {
		return err
	}
	if e.Type != "file" || e.Size != size {
		return errors.New("The remote archive size differs from the completed upload.")
	}
	return nil
}

func publish(ctx context.Context, f remote.FS, c *Config, local, hash string, info archiveInfo) (string, []string, error) {
	dir, err := ownDestination(ctx, f, c)
	if err != nil {
		return "", nil, err
	}
	stamp, _ := time.Parse(time.RFC3339, info.Created)
	name := "ervisio-backup-" + c.ID + "-" + stamp.UTC().Format("20060102T150405Z") + "-" + info.RunID + ".tar.gz"
	file, err := os.Open(local)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	fi, err := file.Stat()
	if err != nil {
		return "", nil, err
	}
	if err = uploadAtomic(ctx, f, path.Join(dir, name), file, fi.Size()); err != nil {
		return "", nil, err
	}
	m := manifest{archiveInfo: info, Archive: name, Bytes: fi.Size(), SHA256: hash}
	m.MAC = m.mac(c.ownerKey)
	b, _ := json.MarshalIndent(m, "", "  ")
	if err = uploadAtomic(ctx, f, path.Join(dir, name+".manifest.json"), bytes.NewReader(b), int64(len(b))); err != nil {
		return "", nil, errors.New("The archive uploaded, but its provenance manifest could not be committed. Previous backups were retained.")
	}
	// Cleanup runs only after both new files are fully committed. A cleanup
	// refusal is a warning, never a reason to discard the new good backup.
	warnings := []string{}
	if err = retain(ctx, f, c, dir, time.Now(), name); err != nil {
		retentionLog(ctx, "retention.failed", "WARN", "Retention cleanup was incomplete; inspect the backup history", info.RunID, 0)
		warnings = append(warnings, "The backup is complete, but retention cleanup was incomplete; existing files were preserved where cleanup failed.")
	}
	return path.Join(dir, name), warnings, nil
}

func retain(ctx context.Context, f remote.FS, c *Config, dir string, now time.Time, protected ...string) error {
	entries, err := f.List(ctx, dir)
	if err != nil {
		return err
	}
	if len(entries) > 5000 {
		return errors.New("Too many destination entries for safe retention.")
	}
	names := map[string]remote.Entry{}
	for _, e := range entries {
		names[e.Name] = e
	}
	pattern := regexp.MustCompile(`^ervisio-backup-` + regexp.QuoteMeta(c.ID) + `-[0-9]{8}T[0-9]{6}Z-[a-f0-9]{32}\.tar\.gz$`)
	type candidate struct {
		m       manifest
		created time.Time
	}
	items := []candidate{}
	for _, e := range entries {
		if e.Type != "file" || !strings.HasSuffix(e.Name, ".manifest.json") {
			continue
		}
		base := strings.TrimSuffix(e.Name, ".manifest.json")
		if !pattern.MatchString(base) {
			continue
		}
		archive, ok := names[base]
		if !ok || archive.Type != "file" {
			continue
		}
		b, err := readRemote(ctx, f, path.Join(dir, e.Name), 64<<10)
		if err != nil {
			continue
		}
		var m manifest
		if decodeStrict(b, &m) != nil || m.JobID != c.ID || m.Schema != "ervisio-backup-v1" || m.Archive != base || m.Bytes != archive.Size || !idPattern.MatchString(m.RunID) || !m.authentic(c.ownerKey) {
			continue
		}
		created, err := time.Parse(time.RFC3339, m.Created)
		if err != nil {
			continue
		}
		items = append(items, candidate{m, created})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].created.Equal(items[j].created) {
			return items[i].m.RunID > items[j].m.RunID
		}
		return items[i].created.After(items[j].created)
	})
	var cutoff time.Time
	if c.MaxAge != nil {
		period := *c.MaxAge
		period.Value = -period.Value
		cutoff = period.Next(now, now.Day())
	}
	for i, item := range items {
		if i == 0 || len(protected) > 0 && item.m.Archive == protected[0] {
			continue
		} // Always retain at least one verified good backup.
		if i < c.KeepLast && (cutoff.IsZero() || !item.created.Before(cutoff)) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		archivePath := path.Join(dir, item.m.Archive)
		current, err := f.Stat(ctx, archivePath)
		if err != nil || current.Type != "file" || current.Size != item.m.Bytes {
			continue
		}
		if err = f.Remove(ctx, archivePath, false); err != nil {
			retentionLog(ctx, "retention.failed", "WARN", "Retention could not delete an expired archive", item.m.RunID, item.m.Bytes)
			return err
		}
		retentionLog(ctx, "retention.deleted", "INFO", "Expired backup archive deleted", item.m.RunID, item.m.Bytes)
		if err = f.Remove(ctx, archivePath+".manifest.json", false); err != nil {
			retentionLog(ctx, "retention.failed", "WARN", "Archive deleted, but its provenance manifest could not be removed", item.m.RunID, 0)
			return err
		}
	}
	return nil
}
