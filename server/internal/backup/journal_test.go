package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/modules/remote"
)

func journalFixture(t *testing.T) (*uploadJournal, *memoryFS, string) {
	t.Helper()
	root := t.TempDir()
	// Every state path is injected under TempDir; never use the operator's profile.
	s := &store{dir: filepath.Join(root, "private")}
	if err := s.init(); err != nil {
		t.Fatal(err)
	}
	c := fixturePlan()
	file := path.Join(c.Destination.Path, "ervisio-backup-"+c.ID, ".ervisio-upload-"+strings.Repeat("c", 32)+".part")
	f := newMemoryFS()
	f.files[c.Destination.Path] = memoryFile{kind: "dir"}
	f.files[path.Dir(file)] = memoryFile{kind: "dir"}
	return &uploadJournal{store: s, config: c}, f, file
}

func TestUploadJournalRecoversExactAuthenticatedIntentAfterRestart(t *testing.T) {
	j, f, file := journalFixture(t)
	if err := j.track(file, 64); err != nil {
		t.Fatal(err)
	}
	journalPath, err := j.location()
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" && fi.Mode().Perm() != 0600 {
		t.Fatal("journal permissions are not private")
	}
	f.files[file] = memoryFile{kind: "file", data: []byte("unfinished")}
	stranger := path.Join(path.Dir(file), ".ervisio-upload-"+strings.Repeat("d", 32)+".part")
	published := path.Join(path.Dir(file), "latest-good.tar.gz")
	f.files[stranger] = memoryFile{kind: "file", data: []byte("not registered")}
	f.files[published] = memoryFile{kind: "file", data: []byte("keep latest good")}
	restarted := &uploadJournal{store: j.store, config: j.config}
	if err = restarted.recover(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if len(f.removed) != 1 || f.removed[0] != file {
		t.Fatalf("removed anything except the registered intent: %v", f.removed)
	}
	for _, kept := range []string{stranger, published} {
		if _, ok := f.files[kept]; !ok {
			t.Fatal("unregistered or published file was removed")
		}
	}
	parts, err := restarted.read()
	if err != nil || len(parts) != 0 {
		t.Fatalf("completed recovery was not cleared: %v %v", parts, err)
	}
}

func TestUploadJournalRejectsForgeryTraversalAndOtherJobWithoutDeleting(t *testing.T) {
	for _, kind := range []string{"mac", "connection", "traversal", "job", "future"} {
		t.Run(kind, func(t *testing.T) {
			j, f, file := journalFixture(t)
			if err := j.track(file, 64); err != nil {
				t.Fatal(err)
			}
			parts, err := j.read()
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "mac":
				parts[0].MAC = strings.Repeat("0", 64)
			case "connection":
				parts[0].Connection = strings.Repeat("e", 16)
			case "traversal":
				parts[0].Path = path.Dir(file) + "/../customer-data.part"
				parts[0].MAC = parts[0].mac(j.config.ownerKey)
			case "job":
				parts[0].JobID = strings.Repeat("e", 32)
				parts[0].MAC = parts[0].mac(j.config.ownerKey)
			case "future":
				parts[0].Created = time.Now().Add(24 * time.Hour).Format(time.RFC3339Nano)
				parts[0].MAC = parts[0].mac(j.config.ownerKey)
			}
			if err = j.write(parts); err != nil {
				t.Fatal(err)
			}
			f.files[file] = memoryFile{kind: "file", data: []byte("partial")}
			if err = j.recover(context.Background(), f); err == nil {
				t.Fatal("unsafe upload journal accepted")
			}
			if len(f.removed) != 0 {
				t.Fatalf("forged intent removed files: %v", f.removed)
			}
		})
	}
}

func TestUploadJournalPreservesOldConnectionDestinationAndExpiredIntent(t *testing.T) {
	for _, kind := range []string{"connection", "destination", "age"} {
		t.Run(kind, func(t *testing.T) {
			j, f, file := journalFixture(t)
			if err := j.track(file, 64); err != nil {
				t.Fatal(err)
			}
			if kind == "age" {
				parts, err := j.read()
				if err != nil {
					t.Fatal(err)
				}
				parts[0].Created = time.Now().Add(-maxUploadPartAge - time.Hour).Format(time.RFC3339Nano)
				parts[0].MAC = parts[0].mac(j.config.ownerKey)
				if err = j.write(parts); err != nil {
					t.Fatal(err)
				}
			} else {
				c := *j.config
				if kind == "connection" {
					c.Destination.Connection = strings.Repeat("d", 16)
				} else {
					c.Destination.Path = "/different-parent"
				}
				j = &uploadJournal{store: j.store, config: &c}
			}
			f.files[file] = memoryFile{kind: "file", data: []byte("preserve")}
			if err := j.recover(context.Background(), f); err != nil {
				t.Fatal(err)
			}
			if len(f.removed) != 0 {
				t.Fatal("a foreign or expired intent was removed")
			}
			parts, err := j.read()
			if err != nil || len(parts) != 1 {
				t.Fatal("foreign/expired provenance was discarded")
			}
		})
	}
}

func TestUploadJournalRefusesChangedFileAndRetainsRecoveryRecord(t *testing.T) {
	for _, kind := range []string{"symlink", "dir", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			j, f, file := journalFixture(t)
			if err := j.track(file, 4); err != nil {
				t.Fatal(err)
			}
			entry := memoryFile{kind: kind, data: []byte("data")}
			if kind == "oversize" {
				entry = memoryFile{kind: "file", data: []byte("larger than expected")}
			}
			f.files[file] = entry
			if err := j.recover(context.Background(), f); err == nil {
				t.Fatal("changed partial upload accepted")
			}
			if len(f.removed) != 0 {
				t.Fatal("changed partial upload was removed")
			}
			parts, err := j.read()
			if err != nil || len(parts) != 1 {
				t.Fatal("retry intent was lost")
			}
		})
	}
}

type journalObservedFS struct {
	*memoryFS
	failure string
	checked bool
}

func (f *journalObservedFS) Stat(ctx context.Context, p string) (remote.Entry, error) {
	if ctx.Err() != nil {
		return remote.Entry{}, ctx.Err()
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > uploadCleanupTimeout || time.Until(deadline) <= 0 {
		return remote.Entry{}, errors.New("cleanup context is not bounded")
	}
	f.checked = true
	if p == f.failure {
		return remote.Entry{}, errors.New("fixture unavailable")
	}
	return f.memoryFS.Stat(ctx, p)
}

func TestUploadJournalCleansWithBoundedContextAfterCallerCancellation(t *testing.T) {
	j, f, file := journalFixture(t)
	if err := j.track(file, 4); err != nil {
		t.Fatal(err)
	}
	f.files[file] = memoryFile{kind: "file", data: []byte("part")}
	observed := &journalObservedFS{memoryFS: f}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := j.recover(ctx, observed); err != nil {
		t.Fatal(err)
	}
	if !observed.checked || len(f.removed) != 1 {
		t.Fatal("caller cancellation prevented bounded owned cleanup")
	}
}

func TestUploadJournalFailurePersistsOnlyOutstandingIntent(t *testing.T) {
	j, f, first := journalFixture(t)
	second := path.Join(path.Dir(first), ".ervisio-upload-"+strings.Repeat("e", 32)+".part")
	for _, file := range []string{first, second} {
		if err := j.track(file, 4); err != nil {
			t.Fatal(err)
		}
		f.files[file] = memoryFile{kind: "file", data: []byte("part")}
	}
	observed := &journalObservedFS{memoryFS: f, failure: second}
	if err := j.recover(context.Background(), observed); err == nil {
		t.Fatal("remote failure was hidden")
	}
	parts, err := j.read()
	if err != nil || len(parts) != 1 || parts[0].Path != second {
		t.Fatalf("wrong outstanding intent: %v %v", parts, err)
	}
	if len(f.removed) != 1 || f.removed[0] != first {
		t.Fatal("recovery removed an uninspected upload")
	}
}

func TestUploadJournalBoundsAndRenameCommitAreDurable(t *testing.T) {
	j, f, file := journalFixture(t)
	if err := j.track(file, 4); err != nil {
		t.Fatal(err)
	}
	// A crash after Rename leaves only the temporary-name intent: recovery must
	// clear the missing name and must never infer/removal the published archive.
	published := path.Join(path.Dir(file), "committed.tar.gz")
	f.files[published] = memoryFile{kind: "file", data: []byte("good")}
	if err := j.recover(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if len(f.removed) != 0 {
		t.Fatal("published file was removed after rename")
	}
	parts := make([]uploadPart, maxUploadParts)
	for i := range parts {
		parts[i] = uploadPart{JobID: j.config.ID, Connection: j.config.Destination.Connection, Destination: j.config.Destination.Path,
			Path: path.Join(path.Dir(file), fmt.Sprintf(".ervisio-upload-%032x.part", i+1)), Size: 4, Created: time.Now().UTC().Format(time.RFC3339Nano)}
		parts[i].MAC = parts[i].mac(j.config.ownerKey)
	}
	if err := j.write(parts); err != nil {
		t.Fatal(err)
	}
	if err := j.track(file, 4); err == nil {
		t.Fatal("unbounded pending upload intents accepted")
	}
	got, err := j.read()
	if err != nil || len(got) != maxUploadParts {
		t.Fatal("journal limit discarded existing recovery records")
	}
	if err := j.clear(parts[0].Path); err != nil {
		t.Fatal(err)
	}
	if got, err = j.read(); err != nil || len(got) != maxUploadParts-1 {
		t.Fatal("successful cleanup was not durably cleared")
	}
	if err = j.track(path.Join(path.Dir(file), "customer.part"), 4); err == nil {
		t.Fatal("non-random temporary file accepted")
	}
}

func TestUploadJournalNeverFollowsPrivateLedgerSymlink(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("production Linux no-follow fixture")
	}
	j, f, file := journalFixture(t)
	if err := j.track(file, 4); err != nil {
		t.Fatal(err)
	}
	location, err := j.location()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(location)
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "other-account-record.json")
	if err = os.WriteFile(foreign, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(location); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(foreign, location); err != nil {
		t.Fatal(err)
	}
	f.files[file] = memoryFile{kind: "file", data: []byte("part")}
	if err = j.recover(context.Background(), f); err == nil {
		t.Fatal("symlink ledger followed")
	}
	if len(f.removed) != 0 {
		t.Fatal("files removed through a foreign ledger")
	}
	var decoded uploadLedger
	if err = json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Parts) != 1 {
		t.Fatal("fixture is not a tracked partial")
	}
}

func TestUploadJournalPreservesReplacedNamespaceAndForeignMarker(t *testing.T) {
	for _, kind := range []string{"ancestor-link", "namespace-link", "foreign-marker"} {
		t.Run(kind, func(t *testing.T) {
			j, f, file := journalFixture(t)
			if err := j.track(file, 4); err != nil {
				t.Fatal(err)
			}
			f.files[file] = memoryFile{kind: "file", data: []byte("part")}
			switch kind {
			case "ancestor-link":
				f.files[j.config.Destination.Path] = memoryFile{kind: "symlink"}
			case "namespace-link":
				f.files[path.Dir(file)] = memoryFile{kind: "symlink"}
			case "foreign-marker":
				f.files[path.Join(path.Dir(file), ".ervisio-backup.json")] = memoryFile{kind: "file", data: []byte(`{"foreign":true}`)}
			}
			if err := j.recover(context.Background(), f); err == nil {
				t.Fatal("replaced or foreign namespace accepted")
			}
			if len(f.removed) != 0 {
				t.Fatal("a remote file was removed through the replaced namespace")
			}
			parts, err := j.read()
			if err != nil || len(parts) != 1 {
				t.Fatal("unsafe namespace lost its pending intent")
			}
		})
	}
}
