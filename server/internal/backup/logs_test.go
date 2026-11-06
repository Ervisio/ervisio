package backup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func logFixture(t *testing.T, endpoint string) *backupLogger {
	t.Helper()
	s := &store{dir: privateTempDir(t)}
	if err := s.init(); err != nil {
		t.Fatal(err)
	}
	c := fixturePlan()
	c.ID = newID()
	token := "evl_test-only-secret"
	c.Logs = &LogConfig{Enabled: true, URL: endpoint, Token: &token, Backups: true, Deletions: true, Stream: "backup", Environment: "test"}
	c.ownerKey = make([]byte, 32)
	return &backupLogger{store: s, config: c}
}

func TestLogEndpointsAndCredentialValidation(t *testing.T) {
	for _, input := range []string{"https://logs.example", "https://logs.example/api/v1", "https://logs.example/api/v1/ingest", "https://logs.example/api/v1/ingest/batch/"} {
		got, err := logsEndpoint(input)
		if err != nil || got != "https://logs.example/api/v1/ingest/batch" {
			t.Fatalf("%q: %q %v", input, got, err)
		}
	}
	for _, input := range []string{"http://logs.example", "https://u:p@logs.example", "https://logs.example?token=secret", "https://logs.example/#secret", "file:///tmp/logs"} {
		if _, err := logsEndpoint(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	if err := validateLogs(&LogConfig{Enabled: true, URL: "https://logs.example", Backups: true}, true); err == nil {
		t.Fatal("accepted enabled logs without token")
	}
}

func TestLogDeliveryRetriesExactIDsAndNeverExportsSecrets(t *testing.T) {
	requests := 0
	var firstID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v1/ingest/batch" || r.Header.Get("Authorization") != "Bearer evl_test-only-secret" {
			t.Error("wrong ingestion protocol")
		}
		var batch struct {
			Events      []backupLogEvent `json:"events"`
			Stream      string           `json:"stream"`
			Environment string           `json:"environment"`
		}
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Error(err)
		}
		if len(batch.Events) != 1 {
			t.Errorf("wrong batch size: %d", len(batch.Events))
			w.WriteHeader(500)
			return
		}
		if requests == 1 {
			firstID = batch.Events[0].ID
			w.WriteHeader(503)
			return
		}
		if batch.Events[0].ID != firstID || batch.Stream != "backup" || batch.Environment != "test" {
			t.Error("retry identity changed")
		}
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"accepted":1,"rejected":0,"status":"durably_queued"}`))
	}))
	defer server.Close()
	l := logFixture(t, server.URL)
	l.emit("backup.completed", "INFO", "Backup completed", map[string]any{"run_id": newID(), "bytes": 123})
	queued, err := os.ReadFile(l.path())
	if err != nil || strings.Contains(string(queued), "secret") || strings.Contains(string(queued), server.URL) {
		t.Fatalf("unsafe retry payload: %v", err)
	}
	l.flush()
	if !l.warning {
		t.Fatal("missing deferred logging warning")
	}
	if _, err := os.Stat(l.path()); err != nil {
		t.Fatal("lost retry queue")
	}
	l.warning = false
	l.flush()
	if l.warning || requests != 2 {
		t.Fatal("delivery did not recover")
	}
	if _, err := os.Stat(l.path()); !os.IsNotExist(err) {
		t.Fatal("acknowledged queue remained")
	}
}

func TestLogOptInFilteringQueueBoundsAndTargetChange(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.WriteHeader(500) }))
	defer server.Close()
	l := logFixture(t, server.URL)
	l.config.Logs.Enabled = false
	l.emit("backup.started", "INFO", "Backup started", map[string]any{})
	l.flush()
	if requests != 0 {
		t.Fatal("disabled logger made a request")
	}
	l.config.Logs.Enabled = true
	l.config.Logs.Backups = false
	l.emit("backup.started", "INFO", "Backup started", map[string]any{})
	if _, err := os.Stat(l.path()); !os.IsNotExist(err) {
		t.Fatal("ignored event persisted")
	}
	ctx := context.WithValue(context.Background(), backupLoggerKey{}, l)
	retentionLog(ctx, "retention.deleted", "INFO", "Expired backup archive deleted", newID(), 50)
	for i := 0; i < 110; i++ {
		l.emit("retention.deleted", "INFO", "Expired backup archive deleted", map[string]any{})
	}
	q, err := l.read()
	if err != nil || len(q.Events) != 100 || !l.warning {
		t.Fatalf("unbounded queue: %d %v", len(q.Events), err)
	}
	otherToken := "evl_other-project"
	l.config.Logs.Token = &otherToken
	l.flush()
	if requests != 0 {
		t.Fatal("sent old project events to new credentials")
	}
	if _, err := os.Stat(l.path()); !os.IsNotExist(err) {
		t.Fatal("stale target queue retained")
	}
}

func TestLogsTokenSealedAndViewIndependent(t *testing.T) {
	l := logFixture(t, "https://logs.example")
	if err := l.store.write(l.config); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(l.store.dir, "state", l.config.ID+".json"))
	if err != nil || strings.Contains(string(data), "evl_test-only-secret") {
		t.Fatal("token stored in plaintext")
	}
	public := view(l.config)
	if public.Logs.Token != nil || !public.Logs.HasToken || l.config.Logs.Token == nil {
		t.Fatal("view leaked or destroyed credentials")
	}
	reloaded, err := l.store.load(l.config.ID)
	if err != nil || reloaded.Logs.Token == nil || *reloaded.Logs.Token != *l.config.Logs.Token {
		t.Fatalf("token did not survive restart: %v", err)
	}
}
