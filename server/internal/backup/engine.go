package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/modules/remote"
)

type Receipt struct {
	ID           string   `json:"id"`
	JobID        string   `json:"jobId"`
	Started      string   `json:"started"`
	Ended        string   `json:"ended,omitempty"`
	Status       string   `json:"status"`
	Archive      string   `json:"archive,omitempty"`
	SHA256       string   `json:"sha256,omitempty"`
	Bytes        int64    `json:"bytes,omitempty"`
	Files        int      `json:"files,omitempty"`
	Databases    int      `json:"databases,omitempty"`
	SkippedLinks int      `json:"skippedLinks,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type engine struct {
	store      store
	withRemote func(context.Context, string, func(context.Context, remote.FS) error) error
}

func (e *engine) init() error {
	if e.withRemote == nil {
		e.withRemote = remote.WithBackupConnection
	}
	return e.store.init()
}

func (s *store) history(id string) ([]Receipt, error) {
	if !idPattern.MatchString(id) {
		return nil, errors.New("Invalid backup ID.")
	}
	b, err := readPrivate(filepath.Join(s.dir, "state", id+".history"), maxInput)
	if errors.Is(err, os.ErrNotExist) {
		return []Receipt{}, nil
	}
	if err != nil {
		return nil, errors.New("Backup history cannot be read safely.")
	}
	var receipts []Receipt
	if err = decodeStrict(b, &receipts); err != nil {
		return nil, err
	}
	return receipts, nil
}

func (s *store) receipt(r Receipt) error {
	items, err := s.history(r.JobID)
	if err != nil {
		return err
	}
	out := []Receipt{r}
	for _, old := range items {
		if old.ID != r.ID {
			out = append(out, old)
		}
		if len(out) >= 20 {
			break
		}
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivate(filepath.Join(s.dir, "state", r.JobID+".history"), b)
}

func (e *engine) run(ctx context.Context, id string, dueOnly bool) (receipt Receipt, err error) {
	unlock, err := waitRunLock(ctx, filepath.Join(e.store.dir, "run.lock"))
	if err != nil {
		return receipt, err
	}
	defer unlock()
	c, err := e.store.load(id)
	if err != nil {
		return receipt, err
	}
	if err = validate(c); err != nil {
		return receipt, err
	}
	if err = cleanOwnedSpools(&e.store, c); err != nil {
		return receipt, err
	}
	now := time.Now()
	next, parseErr := time.Parse(time.RFC3339, c.NextDue)
	if dueOnly && parseErr == nil && now.Before(next) {
		return Receipt{JobID: id, Status: "skipped", Warnings: []string{"The next calendar backup is not due yet."}}, nil
	}
	receipt = Receipt{ID: newID(), JobID: id, Started: now.UTC().Format(time.RFC3339), Status: "running"}
	if err = e.store.receipt(receipt); err != nil {
		return receipt, err
	}
	logger := &backupLogger{store: &e.store, config: c}
	ctx = context.WithValue(ctx, backupLoggerKey{}, logger)
	logger.emit("backup.started", "INFO", "Backup started", map[string]any{"run_id": receipt.ID})
	defer func() {
		receipt.Ended = time.Now().UTC().Format(time.RFC3339)
		if err != nil {
			receipt.Status = "failed"
			receipt.Error = safeRunError(err)
		}
		event, level, message := "backup.completed", "INFO", "Backup completed"
		if receipt.Status == "failed" {
			event, level, message = "backup.failed", "ERROR", "Backup failed; inspect its local history for details"
		}
		logger.emit(event, level, message, map[string]any{"run_id": receipt.ID, "duration_ms": time.Since(now).Milliseconds(), "bytes": receipt.Bytes, "files": receipt.Files, "databases": receipt.Databases, "status": receipt.Status})
		logger.flush()
		if logger.warning {
			receipt.Warnings = append(receipt.Warnings, "ErvisioLogs delivery is pending or its bounded queue is full. The backup result is unchanged; queued events retry on the next run.")
		}
		if persistErr := e.store.receipt(receipt); err == nil && persistErr != nil {
			err = errors.New("The backup finished, but its local history could not be saved.")
		}
	}()
	if err = ctx.Err(); err != nil {
		return receipt, err
	}
	free, err := availableBytes(e.store.dir)
	if err != nil || free <= reserveBytes+2<<20 {
		return receipt, errors.New("There is insufficient free space for the private backup spool.")
	}
	limit := c.MaxBytes
	if free-reserveBytes < limit {
		limit = free - reserveBytes
	}
	spool, err := os.MkdirTemp(e.store.dir, "spool-"+c.ID+"-")
	if err != nil {
		return receipt, err
	}
	defer os.RemoveAll(spool)
	if err = os.Chmod(spool, 0700); err != nil {
		return receipt, err
	}
	if err = markSpool(spool, c); err != nil {
		return receipt, err
	}
	b := &budget{limit: limit, dir: spool}
	local, hash, info, err := buildArchive(ctx, c, receipt.ID, spool, e.store.dir, b)
	if err != nil {
		return receipt, err
	}
	var warnings []string
	err = e.withRemote(ctx, c.Destination.Connection, func(ctx context.Context, f remote.FS) error {
		journal := &uploadJournal{store: &e.store, config: c}
		if err := journal.recover(ctx, f); err != nil {
			return err
		}
		ctx = context.WithValue(ctx, journalKey{}, journal)
		var err error
		receipt.Archive, warnings, err = publish(ctx, f, c, local, hash, info)
		return err
	})
	if err != nil {
		return receipt, err
	}
	fi, err := os.Stat(local)
	if err != nil {
		return receipt, err
	}
	receipt.Status = "ok"
	receipt.SHA256 = hash
	receipt.Bytes = fi.Size()
	receipt.Files = info.Files
	receipt.Databases = info.Databases
	receipt.SkippedLinks = info.SkippedLinks
	receipt.Warnings = warnings
	if info.SkippedLinks > 0 {
		receipt.Warnings = append(receipt.Warnings, "Symbolic links were skipped; their targets were not followed.")
	}
	// Save only the due time from this run; a simultaneous settings edit must
	// never be overwritten by the worker's older snapshot.
	configUnlock, lockErr := lockFile(filepath.Join(e.store.dir, "config.lock"))
	if lockErr == nil {
		current, loadErr := e.store.load(id)
		if loadErr == nil && current.Frequency == c.Frequency {
			current.NextDue = nextDue(c, now).Format(time.RFC3339)
			if writeErr := e.store.write(current); writeErr != nil {
				receipt.Warnings = append(receipt.Warnings, "The next due time could not be saved; review the plan before its next run.")
			}
		}
		configUnlock()
	} else {
		receipt.Warnings = append(receipt.Warnings, "The next due time could not be saved while settings were changing.")
	}
	return receipt, nil
}

func safeRunError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "The backup was cancelled; previous good backups were retained."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "The backup reached its time limit; previous good backups were retained."
	}
	// Our own validation/worker messages never contain passwords or raw client
	// stderr. Remote protocol errors are reduced to a stable, non-secret message.
	s := err.Error()
	if !text(s, 500) || strings.Contains(s, "password=") || strings.Contains(s, "://") {
		return "The backup could not complete. Check access, remote availability and free space; previous backups were retained."
	}
	return s
}
