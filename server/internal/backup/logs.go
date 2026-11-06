package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Logging is explicitly enabled per plan. Credentials never enter views,
// receipts, events or the retry queue.
type LogConfig struct {
	Enabled     bool    `json:"enabled"`
	URL         string  `json:"url"`
	Token       *string `json:"token,omitempty"`
	HasToken    bool    `json:"hasToken,omitempty"`
	ClearToken  bool    `json:"clearToken,omitempty"`
	Backups     bool    `json:"backups"`
	Deletions   bool    `json:"deletions"`
	Stream      string  `json:"stream,omitempty"`
	Environment string  `json:"environment,omitempty"`
}

func logsEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(raw) > 2048 {
		return "", errors.New("Choose an ErvisioLogs HTTPS instance URL without credentials or query parameters.")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())) {
		return "", errors.New("ErvisioLogs requires HTTPS (HTTP is allowed only on loopback).")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	if strings.HasSuffix(u.Path, "/api/v1/ingest/batch") {
		return u.String(), nil
	}
	if strings.HasSuffix(u.Path, "/api/v1/ingest") {
		u.Path += "/batch"
	} else {
		if !strings.HasSuffix(u.Path, "/api/v1") {
			u.Path += "/api/v1"
		}
		u.Path += "/ingest/batch"
	}
	u.RawPath = ""
	return u.String(), nil
}

func validateLogs(c *LogConfig, requireToken bool) error {
	if c == nil {
		return nil
	}
	if c.Token != nil && (!text(*c.Token, 4096) || strings.ContainsAny(*c.Token, " \t")) {
		return errors.New("Invalid ErvisioLogs API token.")
	}
	if !c.Enabled {
		return nil
	}
	if _, err := logsEndpoint(c.URL); err != nil {
		return err
	}
	if !text(c.Stream, 100) || !text(c.Environment, 100) {
		return errors.New("ErvisioLogs stream and environment must be at most 100 characters.")
	}
	if !c.Backups && !c.Deletions {
		return errors.New("Choose backup events, retention deletions, or both.")
	}
	if requireToken && (c.Token == nil || *c.Token == "") {
		return errors.New("Set an ErvisioLogs project token with logs:ingest permission.")
	}
	return nil
}

type backupLogEvent struct {
	ID         string         `json:"id"`
	Timestamp  string         `json:"timestamp"`
	Level      string         `json:"level"`
	Event      string         `json:"event"`
	Message    string         `json:"message"`
	Service    string         `json:"service"`
	Attributes map[string]any `json:"attributes"`
}

type logOutbox struct {
	Target string           `json:"target"`
	Events []backupLogEvent `json:"events"`
}

func logsTarget(c *LogConfig) string {
	endpoint, _ := logsEndpoint(c.URL)
	// Bind retries to the original endpoint, project token and stream. Changing
	// settings must never disclose queued events to another project.
	b, _ := json.Marshal([]string{endpoint, *c.Token, c.Stream, c.Environment})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type backupLogger struct {
	store   *store
	config  *Config
	warning bool
}

func (l *backupLogger) path() string {
	return filepath.Join(l.store.dir, "state", l.config.ID+".logs-outbox")
}
func (l *backupLogger) read() (logOutbox, error) {
	var q logOutbox
	data, err := readPrivate(l.path(), 512<<10)
	if errors.Is(err, os.ErrNotExist) {
		return q, nil
	}
	if err != nil {
		return q, err
	}
	if err := decodeStrict(data, &q); err != nil {
		return q, err
	}
	if len(q.Events) > 100 {
		return q, errors.New("Invalid log queue.")
	}
	return q, nil
}

func (l *backupLogger) emit(kind, level, message string, attributes map[string]any) {
	c := l.config.Logs
	if c == nil || !c.Enabled || validateLogs(c, true) != nil {
		return
	}
	if strings.HasPrefix(kind, "backup.") && !c.Backups || strings.HasPrefix(kind, "retention.") && !c.Deletions {
		return
	}
	q, err := l.read()
	if err != nil {
		l.warning = true
		return
	}
	target := logsTarget(c)
	if q.Target != target {
		q = logOutbox{Target: target}
	}
	if len(q.Events) >= 100 {
		l.warning = true
		return
	}
	attributes["plan_id"] = l.config.ID
	q.Events = append(q.Events, backupLogEvent{ID: newID(), Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: level, Event: kind, Message: message, Service: "ervisio-backup", Attributes: attributes})
	b, err := json.Marshal(q)
	if err != nil || atomicPrivate(l.path(), b) != nil {
		l.warning = true
	}
}

// Called while the plan's run lock is held. A single bounded request drains
// up to 100 records. Transient failures retain the exact IDs for next run.
// No background process, unbounded retries or dependency on successful logs.
func (l *backupLogger) flush() {
	c := l.config.Logs
	if c == nil || !c.Enabled || validateLogs(c, true) != nil {
		return
	}
	q, err := l.read()
	if err != nil {
		l.warning = true
		return
	}
	if len(q.Events) == 0 {
		return
	}
	if q.Target != logsTarget(c) {
		if os.Remove(l.path()) != nil {
			l.warning = true
		}
		return
	}
	b, err := json.Marshal(map[string]any{"events": q.Events, "stream": c.Stream, "environment": c.Environment})
	if err != nil {
		l.warning = true
		return
	}
	endpoint, _ := logsEndpoint(c.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		l.warning = true
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+*c.Token)
	client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		l.warning = true
		return
	}
	defer resp.Body.Close()
	var result struct {
		Accepted int    `json:"accepted"`
		Rejected int    `json:"rejected"`
		Status   string `json:"status"`
	}
	if resp.StatusCode != http.StatusAccepted || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result) != nil || result.Accepted != len(q.Events) || result.Rejected != 0 || result.Status != "durably_queued" {
		l.warning = true
		return
	}
	if os.Remove(l.path()) != nil {
		l.warning = true
	}
}

type backupLoggerKey struct{}

func retentionLog(ctx context.Context, kind, level, message string, runID string, size int64) {
	if logger, ok := ctx.Value(backupLoggerKey{}).(*backupLogger); ok {
		logger.emit(kind, level, message, map[string]any{"archive_run_id": runID, "bytes": size})
	}
}
