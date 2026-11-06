package backup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/modules/remote"
)

const (
	maxUploadParts        = 1000
	maxUploadJournalBytes = 8 << 20
	maxUploadPartAge      = 90 * 24 * time.Hour
	uploadCleanupTimeout  = 10 * time.Second
)

type journalKey struct{}
type uploadJournal struct {
	store  *store
	config *Config
}
type uploadPart struct {
	JobID       string `json:"jobId"`
	Connection  string `json:"connection"`
	Destination string `json:"destination"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Created     string `json:"created"`
	MAC         string `json:"mac"`
}
type uploadLedger struct {
	Schema string       `json:"schema"`
	Parts  []uploadPart `json:"parts"`
}

var uploadPartName = regexp.MustCompile(`^\.ervisio-upload-[a-f0-9]{32}\.part$`)

func (part uploadPart) mac(key []byte) string {
	part.MAC = ""
	body, _ := json.Marshal(part)
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("ervisio-backup-upload-intent-v1\n"))
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func (part uploadPart) valid(key []byte, job string) bool {
	destination, err := cleanDestination(part.Destination)
	if err != nil || destination != part.Destination || part.JobID != job ||
		!connectionPattern.MatchString(part.Connection) || part.Size < 0 || part.Size > 100<<30 ||
		part.Path != path.Clean(part.Path) ||
		path.Dir(part.Path) != path.Join(destination, "ervisio-backup-"+job) ||
		!uploadPartName.MatchString(path.Base(part.Path)) {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, part.Created)
	if err != nil || created.IsZero() || created.After(time.Now().Add(5*time.Minute)) {
		return false
	}
	expected, err := hex.DecodeString(part.mac(key))
	if err != nil {
		return false
	}
	actual, err := hex.DecodeString(part.MAC)
	return err == nil && hmac.Equal(expected, actual)
}

func (j *uploadJournal) location() (string, error) {
	if j == nil || j.store == nil || j.config == nil || !idPattern.MatchString(j.config.ID) ||
		len(j.config.ownerKey) != 32 || !connectionPattern.MatchString(j.config.Destination.Connection) ||
		!filepath.IsAbs(j.store.dir) {
		return "", errors.New("The private upload journal is unavailable.")
	}
	destination, err := cleanDestination(j.config.Destination.Path)
	if err != nil || destination != j.config.Destination.Path {
		return "", errors.New("The upload journal destination is invalid.")
	}
	for _, directory := range []string{j.store.dir, filepath.Join(j.store.dir, "state")} {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !privateOwner(info) {
			return "", errors.New("The upload journal requires a private account-owned directory.")
		}
	}
	return filepath.Join(j.store.dir, "state", j.config.ID+".parts"), nil
}

func (j *uploadJournal) read() ([]uploadPart, error) {
	file, err := j.location()
	if err != nil {
		return nil, err
	}
	body, err := readPrivate(file, maxUploadJournalBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return []uploadPart{}, nil
	}
	if err != nil {
		return nil, errors.New("The private upload journal cannot be read safely.")
	}
	var ledger uploadLedger
	if decodeStrict(body, &ledger) != nil || ledger.Schema != "ervisio-upload-journal-v1" || len(ledger.Parts) > maxUploadParts {
		return nil, errors.New("The private upload journal is invalid or exceeds its limit.")
	}
	seen := map[string]bool{}
	for _, part := range ledger.Parts {
		key := part.Connection + "\n" + part.Path
		if !part.valid(j.config.ownerKey, j.config.ID) || seen[key] {
			return nil, errors.New("The private upload journal failed its provenance check; no remote files were removed.")
		}
		seen[key] = true
	}
	return ledger.Parts, nil
}

func (j *uploadJournal) write(parts []uploadPart) error {
	file, err := j.location()
	if err != nil {
		return err
	}
	if len(parts) > maxUploadParts {
		return errors.New("The upload recovery journal is full. Review its pending partial uploads before retrying.")
	}
	body, err := json.Marshal(uploadLedger{Schema: "ervisio-upload-journal-v1", Parts: parts})
	if err != nil || len(body) > maxUploadJournalBytes {
		return errors.New("The upload journal exceeds its size limit.")
	}
	if err = atomicPrivate(file, body); err != nil {
		return errors.New("The upload intent could not be saved durably; no new upload was started.")
	}
	// The intent must survive a crash before Create. atomicPrivate syncs the file;
	// sync its directory too so the rename itself is durable on Linux.
	if runtime.GOOS == "linux" {
		directory, err := openProtected(filepath.Dir(file), os.O_RDONLY, 0)
		if err != nil {
			return errors.New("The upload journal directory cannot be synchronized.")
		}
		defer directory.Close()
		info, err := directory.Stat()
		if err != nil || !info.IsDir() || !privateOwner(info) {
			return errors.New("The upload journal directory changed while saving.")
		}
		if err = directory.Sync(); err != nil {
			return errors.New("The upload journal directory cannot be synchronized.")
		}
	}
	return nil
}

// track commits authenticated intent before a remote temporary file is created.
func (j *uploadJournal) track(file string, size int64) error {
	parts, err := j.read()
	if err != nil {
		return err
	}
	part := uploadPart{JobID: j.config.ID, Connection: j.config.Destination.Connection, Destination: j.config.Destination.Path,
		Path: file, Size: size, Created: time.Now().UTC().Format(time.RFC3339Nano)}
	part.MAC = part.mac(j.config.ownerKey)
	if !part.valid(j.config.ownerKey, j.config.ID) || size > j.config.MaxBytes {
		return errors.New("The remote upload intent is outside this backup plan's namespace or size limit.")
	}
	for _, old := range parts {
		if old.Connection == part.Connection && old.Path == part.Path {
			if old.Size != size {
				return errors.New("An existing upload intent has a different size.")
			}
			return nil
		}
	}
	return j.write(append(parts, part))
}

// clear is called only after successful removal or rename; a crashed commit is
// harmless because recovery sees that the temporary name no longer exists.
func (j *uploadJournal) clear(file string) error {
	parts, err := j.read()
	if err != nil {
		return err
	}
	remaining := make([]uploadPart, 0, len(parts))
	for _, part := range parts {
		if part.Connection == j.config.Destination.Connection && part.Destination == j.config.Destination.Path && part.Path == file {
			continue
		}
		remaining = append(remaining, part)
	}
	if len(remaining) == len(parts) {
		return nil
	}
	return j.write(remaining)
}

// Validate the current remote namespace as well as the authenticated lexical
// path. Recovery must never follow a directory replaced by a remote symlink.
func (j *uploadJournal) inspectParent(ctx context.Context, f remote.FS, parent string) (bool, error) {
	prefix := "/"
	components := append([]string{""}, strings.Split(strings.Trim(parent, "/"), "/")...)
	for _, component := range components {
		if component != "" {
			prefix = path.Join(prefix, component)
		}
		entry, err := f.Stat(ctx, prefix)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, errors.New("The previous upload's remote namespace could not be inspected safely.")
		}
		if entry.Type != "dir" {
			return false, errors.New("The previous upload's remote namespace contains a link or non-directory; nothing there was removed.")
		}
	}
	// The first marker upload can itself have been interrupted. Absence is
	// allowed only with authenticated local intent and the real directory chain.
	actual, err := readRemote(ctx, f, path.Join(parent, ".ervisio-backup.json"), 4096)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, errors.New("The previous upload's ownership marker cannot be read safely.")
	}
	digest := sha256.Sum256(j.config.ownerKey)
	expected, _ := json.Marshal(struct {
		Schema string `json:"schema"`
		JobID  string `json:"jobId"`
		KeyID  string `json:"keyId"`
	}{"ervisio-backup-owner-v1", j.config.ID, hex.EncodeToString(digest[:])})
	if !bytes.Equal(actual, expected) {
		return false, errors.New("The previous upload's remote namespace changed ownership; nothing there was removed.")
	}
	return true, nil
}

// recover operates only through the newly opened connection for this run.
// Past destinations and old intents stay untouched, never guessed by prefix.
func (j *uploadJournal) recover(parent context.Context, f remote.FS) error {
	parts, err := j.read()
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), uploadCleanupTimeout)
	stop := context.AfterFunc(ctx, func() { _ = f.Close() })
	defer func() { stop(); cancel() }()
	now := time.Now()
	remaining := make([]uploadPart, 0, len(parts))
	changed := false
	finish := func(cause error) error {
		if changed {
			if err := j.write(remaining); err != nil {
				return err
			}
		}
		return cause
	}
	for index, part := range parts {
		created, _ := time.Parse(time.RFC3339Nano, part.Created)
		if part.Connection != j.config.Destination.Connection || part.Destination != j.config.Destination.Path || created.Before(now.Add(-maxUploadPartAge)) {
			remaining = append(remaining, part)
			continue
		}
		if err = ctx.Err(); err != nil {
			remaining = append(remaining, parts[index:]...)
			return finish(err)
		}
		exists, inspectErr := j.inspectParent(ctx, f, path.Dir(part.Path))
		if inspectErr != nil {
			remaining = append(remaining, parts[index:]...)
			return finish(inspectErr)
		}
		if !exists {
			changed = true
			continue
		}
		entry, statErr := f.Stat(ctx, part.Path)
		if errors.Is(statErr, fs.ErrNotExist) {
			changed = true
			continue
		}
		if statErr != nil {
			remaining = append(remaining, parts[index:]...)
			return finish(errors.New("A previous partial upload could not be inspected; its recovery intent was preserved."))
		}
		if entry.Type != "file" || entry.Size < 0 || entry.Size > part.Size {
			remaining = append(remaining, parts[index:]...)
			return finish(errors.New("A previous partial upload changed type or exceeded its recorded size; it was preserved."))
		}
		if err = f.Remove(ctx, part.Path, false); err != nil && !errors.Is(err, fs.ErrNotExist) {
			remaining = append(remaining, parts[index:]...)
			return finish(errors.New("A previous partial upload could not be removed; its recovery intent was preserved."))
		}
		changed = true
	}
	return finish(nil)
}
