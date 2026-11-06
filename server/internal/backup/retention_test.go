package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/modules/remote"
)

type memoryFile struct {
	data []byte
	kind string
}
type memoryFS struct {
	files      map[string]memoryFile
	removed    []string
	failUpload bool
}

func newMemoryFS() *memoryFS { return &memoryFS{files: map[string]memoryFile{"/": {kind: "dir"}}} }
func (f *memoryFS) List(_ context.Context, dir string) ([]remote.Entry, error) {
	items := []remote.Entry{}
	for p, file := range f.files {
		if p != "/" && path.Dir(p) == dir {
			items = append(items, remote.Entry{Name: path.Base(p), Path: p, Type: file.kind, Size: int64(len(file.data))})
		}
	}
	return items, nil
}
func (f *memoryFS) Stat(_ context.Context, p string) (remote.Entry, error) {
	file, ok := f.files[p]
	if !ok {
		return remote.Entry{}, fs.ErrNotExist
	}
	return remote.Entry{Name: path.Base(p), Path: p, Type: file.kind, Size: int64(len(file.data))}, nil
}
func (f *memoryFS) Mkdir(_ context.Context, p string) error {
	if _, ok := f.files[p]; ok {
		return fs.ErrExist
	}
	f.files[p] = memoryFile{kind: "dir"}
	return nil
}
func (f *memoryFS) Rename(_ context.Context, from, to string) error {
	if _, ok := f.files[to]; ok {
		return fs.ErrExist
	}
	file, ok := f.files[from]
	if !ok {
		return fs.ErrNotExist
	}
	f.files[to] = file
	delete(f.files, from)
	return nil
}
func (f *memoryFS) Remove(_ context.Context, p string, _ bool) error {
	if _, ok := f.files[p]; !ok {
		return fs.ErrNotExist
	}
	delete(f.files, p)
	f.removed = append(f.removed, p)
	return nil
}
func (f *memoryFS) Open(_ context.Context, p string) (io.ReadCloser, error) {
	file, ok := f.files[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(file.data)), nil
}

type memoryWriter struct {
	*bytes.Buffer
	close func([]byte) error
	fail  bool
}

func (w *memoryWriter) Write(b []byte) (int, error) {
	if w.fail {
		return 0, errors.New("fixture upload failure")
	}
	return w.Buffer.Write(b)
}
func (w *memoryWriter) Close() error { return w.close(w.Bytes()) }
func (f *memoryFS) Create(_ context.Context, p string, _ int64) (io.WriteCloser, error) {
	if _, ok := f.files[p]; ok {
		return nil, fs.ErrExist
	}
	f.files[p] = memoryFile{kind: "file"}
	return &memoryWriter{Buffer: &bytes.Buffer{}, fail: f.failUpload, close: func(b []byte) error { f.files[p] = memoryFile{data: append([]byte{}, b...), kind: "file"}; return nil }}, nil
}
func (*memoryFS) Close() error { return nil }

func fixturePlan() *Config {
	return &Config{ID: strings.Repeat("a", 32), Name: "Test", Mode: "folders", Folders: []string{"/source"}, Destination: Destination{Connection: strings.Repeat("b", 16), Path: "/backups"}, Frequency: Period{1, "days"}, KeepLast: 1, MaxBytes: 1 << 20, ownerKey: bytes.Repeat([]byte{9}, 32)}
}

func addBackup(f *memoryFS, c *Config, dir string, when time.Time, run string, authentic bool) string {
	name := "ervisio-backup-" + c.ID + "-" + when.UTC().Format("20060102T150405Z") + "-" + run + ".tar.gz"
	m := manifest{archiveInfo: archiveInfo{Schema: "ervisio-backup-v1", JobID: c.ID, RunID: run, Created: when.UTC().Format(time.RFC3339)}, Archive: name, Bytes: 4, SHA256: strings.Repeat("0", 64)}
	m.MAC = m.mac(c.ownerKey)
	if !authentic {
		m.MAC = strings.Repeat("0", 64)
	}
	b, _ := json.Marshal(m)
	f.files[path.Join(dir, name)] = memoryFile{data: []byte("data"), kind: "file"}
	f.files[path.Join(dir, name+".manifest.json")] = memoryFile{data: b, kind: "file"}
	return name
}

func TestRetentionOnlyAuthenticOwnedPairs(t *testing.T) {
	f := newMemoryFS()
	c := fixturePlan()
	dir := "/backups/ervisio-backup-" + c.ID
	f.files[dir] = memoryFile{kind: "dir"}
	now := time.Now().UTC().Truncate(time.Second)
	old := addBackup(f, c, dir, now.Add(-48*time.Hour), strings.Repeat("1", 32), true)
	latest := addBackup(f, c, dir, now, strings.Repeat("2", 32), true)
	forged := addBackup(f, c, dir, now.Add(-72*time.Hour), strings.Repeat("3", 32), false)
	f.files[path.Join(dir, "customer-important.sql")] = memoryFile{data: []byte("untouched"), kind: "file"}
	if err := retain(context.Background(), f, c, dir, now); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{latest, forged, "customer-important.sql"} {
		if _, ok := f.files[path.Join(dir, name)]; !ok {
			t.Fatalf("retention removed unowned or newest file %s", name)
		}
	}
	if _, ok := f.files[path.Join(dir, old)]; ok {
		t.Fatal("owned older backup not removed")
	}
	if len(f.removed) != 2 {
		t.Fatalf("unexpected removals: %v", f.removed)
	}
}

func TestRetentionAlwaysKeepsOneEvenPastAge(t *testing.T) {
	f := newMemoryFS()
	c := fixturePlan()
	c.MaxAge = &Period{1, "minutes"}
	dir := "/backups/owned"
	now := time.Now().UTC()
	name := addBackup(f, c, dir, now.Add(-100*time.Hour), strings.Repeat("1", 32), true)
	if err := retain(context.Background(), f, c, dir, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.files[path.Join(dir, name)]; !ok {
		t.Fatal("last good backup was deleted")
	}
}

func TestAtomicUploadRefusesOverwriteAndFailureLeavesGood(t *testing.T) {
	f := newMemoryFS()
	f.files["/good.tar.gz"] = memoryFile{data: []byte("old-good"), kind: "file"}
	if err := uploadAtomic(context.Background(), f, "/good.tar.gz", strings.NewReader("new"), 3); err == nil {
		t.Fatal("overwrite allowed")
	}
	f.failUpload = true
	if err := uploadAtomic(context.Background(), f, "/new.tar.gz", strings.NewReader("new"), 3); err == nil {
		t.Fatal("failed upload succeeded")
	}
	if string(f.files["/good.tar.gz"].data) != "old-good" {
		t.Fatal("old good archive changed")
	}
	if _, ok := f.files["/new.tar.gz"]; ok {
		t.Fatal("partial upload published")
	}
	for name := range f.files {
		if strings.Contains(name, ".ervisio-upload-") {
			t.Fatal("owned partial upload not cleaned")
		}
	}
}

func TestOwnershipMarkerChangeRefusesDestination(t *testing.T) {
	f := newMemoryFS()
	c := fixturePlan()
	dir := "/backups/ervisio-backup-" + c.ID
	f.files[dir] = memoryFile{kind: "dir"}
	f.files[path.Join(dir, ".ervisio-backup.json")] = memoryFile{data: []byte(`{"foreign":true}`), kind: "file"}
	if _, err := ownDestination(context.Background(), f, c); err == nil {
		t.Fatal("foreign ownership marker accepted")
	}
	if len(f.removed) != 0 {
		t.Fatal("destination check deleted data")
	}
}

func TestDestinationRejectsSymlinkComponent(t *testing.T) {
	f := newMemoryFS()
	f.files["/backups"] = memoryFile{kind: "symlink"}
	if err := remoteDirectory(context.Background(), f, "/backups/job"); err == nil {
		t.Fatal("destination symlink followed")
	}
}
