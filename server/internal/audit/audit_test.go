package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type testCfg struct {
	on   bool
	days int
}

func (c *testCfg) AuditEnabled() bool      { return c.on }
func (c *testCfg) AuditRetentionDays() int { return c.days }

func newLog(t *testing.T, now time.Time) (*Log, *testCfg, *time.Time) {
	t.Helper()
	cfg := &testCfg{on: true, days: 90}
	l := New(filepath.Join(t.TempDir(), "audit"), cfg)
	cur := now
	l.now = func() time.Time { return cur }
	return l, cfg, &cur
}

func day(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

func TestAddWritesJSONLinesAndRotatesDaily(t *testing.T) {
	l, _, cur := newLog(t, day(2026, 10, 1, 23))
	for i := 0; i < 2; i++ {
		if err := l.Add(Entry{User: "u", Source: SourcePlugin, Plugin: "docker", Action: "http", Target: fmt.Sprintf("POST /x/%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	*cur = day(2026, 10, 2, 0).Add(time.Minute)
	if err := l.Add(Entry{User: "u", Source: SourceCore, Action: "login"}); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(l.Dir())
	if len(files) != 2 || files[0].Name() != "audit-2026-10-01.jsonl" || files[1].Name() != "audit-2026-10-02.jsonl" {
		t.Fatalf("files: %v", files)
	}
	b, _ := os.ReadFile(filepath.Join(l.Dir(), files[0].Name()))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("%q", b)
	}
	var e Entry
	if err := json.Unmarshal([]byte(lines[1]), &e); err != nil || e.Target != "POST /x/1" || e.Result != OK || e.Time.IsZero() {
		t.Fatalf("%v %+v", err, e)
	}
	// Private files in a private folder.
	if runtime.GOOS != "windows" { // POSIX modes mean nothing on Windows
		if fi, _ := os.Stat(filepath.Join(l.Dir(), files[0].Name())); fi.Mode().Perm() != 0o600 {
			t.Fatalf("file mode %v", fi.Mode())
		}
		if fi, _ := os.Stat(l.Dir()); fi.Mode().Perm() != 0o700 {
			t.Fatalf("dir mode %v", fi.Mode())
		}
	}
}

func TestAppendOnlyAcrossReopen(t *testing.T) {
	l, cfg, _ := newLog(t, day(2026, 10, 1, 12))
	l.Add(Entry{User: "a", Source: SourceCore, Action: "login"})
	l.Close()
	l2 := New(l.Dir(), cfg)
	l2.now = l.now
	l2.Add(Entry{User: "b", Source: SourceCore, Action: "login"})
	got, _, _ := l2.List(Query{})
	if len(got) != 2 || got[0].User != "b" || got[1].User != "a" {
		t.Fatalf("%+v", got)
	}
}

func TestDisabledWritesNothing(t *testing.T) {
	l, cfg, _ := newLog(t, day(2026, 10, 1, 12))
	cfg.on = false
	if err := l.Add(Entry{User: "a", Source: SourceCore, Action: "login"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(l.Dir()); !os.IsNotExist(err) {
		t.Fatal("the folder was created while disabled")
	}
	cfg.on = true
	l.Add(Entry{User: "a", Source: SourceCore, Action: "login"})
	if got, _, _ := l.List(Query{}); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestRetention(t *testing.T) {
	l, cfg, cur := newLog(t, day(2026, 10, 2, 12))
	os.MkdirAll(l.Dir(), 0o700)
	for _, d := range []string{"2026-06-01", "2026-07-01", "2026-07-04", "2026-09-30", "2026-10-02"} {
		os.WriteFile(filepath.Join(l.Dir(), "audit-"+d+".jsonl"), []byte("{}\n"), 0o600)
	}
	os.WriteFile(filepath.Join(l.Dir(), "notes.txt"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(l.Dir(), "audit-bad.jsonl"), []byte("x"), 0o600)
	cfg.days = 90 // keeps from 2026-07-04 on
	l.Prune()
	names := func() string {
		es, _ := os.ReadDir(l.Dir())
		var n []string
		for _, e := range es {
			n = append(n, e.Name())
		}
		return strings.Join(n, " ")
	}
	if got := names(); got != "audit-2026-07-04.jsonl audit-2026-09-30.jsonl audit-2026-10-02.jsonl audit-bad.jsonl notes.txt" {
		t.Fatalf("after 90 days: %s", got)
	}
	cfg.days = 7
	l.Prune()
	if got := names(); got != "audit-2026-09-30.jsonl audit-2026-10-02.jsonl audit-bad.jsonl notes.txt" {
		t.Fatalf("after 7 days: %s", got)
	}
	cfg.days = 0 // forever
	*cur = day(2030, 1, 1, 0)
	l.Prune()
	if got := names(); !strings.Contains(got, "audit-2026-09-30.jsonl") {
		t.Fatalf("keep forever: %s", got)
	}
}

func fill(t *testing.T, l *Log, cur *time.Time, days, perDay int) {
	t.Helper()
	start := *cur
	for d := 0; d < days; d++ {
		for i := 0; i < perDay; i++ {
			*cur = start.AddDate(0, 0, d).Add(time.Duration(i) * time.Minute)
			user, plugin := "alice", "docker"
			if i%2 == 1 {
				user, plugin = "bob", "other"
			}
			if err := l.Add(Entry{User: user, Source: SourcePlugin, Plugin: plugin, Action: "http", Target: fmt.Sprintf("POST /d%d/i%d", d, i)}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestListFiltersNewestFirstAndPages(t *testing.T) {
	l, _, cur := newLog(t, day(2026, 9, 28, 8))
	fill(t, l, cur, 5, 10) // 28 Sep .. 2 Oct, 10 per day, alternating users
	*cur = day(2026, 10, 3, 0)
	all, next, err := l.List(Query{Limit: 1000})
	if err != nil || len(all) != 50 || next != "" {
		t.Fatalf("%d entries, next %q, %v", len(all), next, err)
	}
	for i := 1; i < len(all); i++ {
		if all[i].Time.After(all[i-1].Time) {
			t.Fatalf("not newest first at %d", i)
		}
	}
	// Pages of 7 cover everything once, across files.
	seen := map[string]bool{}
	cursor, pages := "", 0
	for {
		page, nx, err := l.List(Query{Limit: 7, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page {
			if seen[e.Target] {
				t.Fatalf("%s twice", e.Target)
			}
			seen[e.Target] = true
		}
		pages++
		if nx == "" {
			break
		}
		cursor = nx
		if pages > 20 {
			t.Fatal("paging does not end")
		}
	}
	if len(seen) != 50 {
		t.Fatalf("%d entries over %d pages", len(seen), pages)
	}
	// Filters.
	got, _, _ := l.List(Query{Plugin: "other", Limit: 1000})
	if len(got) != 25 {
		t.Fatalf("plugin filter: %d", len(got))
	}
	got, _, _ = l.List(Query{User: "alice", Plugin: "docker", Limit: 1000})
	if len(got) != 25 || got[0].User != "alice" {
		t.Fatalf("user filter: %d", len(got))
	}
	got, _, _ = l.List(Query{Since: day(2026, 9, 30, 0), Until: day(2026, 10, 1, 0), Limit: 1000})
	if len(got) != 10 { // 30 Sep
		t.Fatalf("time filter: %d", len(got))
	}
	got, _, _ = l.List(Query{Text: "D3/I4", Limit: 1000})
	if len(got) != 1 || got[0].Target != "POST /d3/i4" {
		t.Fatalf("text filter: %+v", got)
	}
	if got, _, _ = l.List(Query{Source: SourceCore}); len(got) != 0 {
		t.Fatal("source filter")
	}
	// Bad cursor.
	for _, c := range []string{"x", "2026-10-01", "2026-10-01:-1", "nope:3", "2026-10-01:x"} {
		if _, _, err := l.List(Query{Cursor: c}); err != ErrCursor {
			t.Errorf("cursor %q: %v", c, err)
		}
	}
	// Limits.
	if got, _, _ = l.List(Query{}); len(got) != DefaultLimit && len(got) != 50 {
		t.Fatalf("default limit: %d", len(got))
	}
}

func TestListSurvivesDamagedLines(t *testing.T) {
	l, _, _ := newLog(t, day(2026, 10, 1, 12))
	l.Add(Entry{User: "a", Source: SourceCore, Action: "login"})
	f := filepath.Join(l.Dir(), "audit-2026-10-01.jsonl")
	fh, _ := os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString("not json\n{\"time\":\n")
	fh.Close()
	l.Add(Entry{User: "b", Source: SourceCore, Action: "login"})
	if got, _, err := l.List(Query{}); err != nil || len(got) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestEachOldestFirst(t *testing.T) {
	l, _, cur := newLog(t, day(2026, 9, 30, 8))
	fill(t, l, cur, 3, 4)
	var targets []string
	if err := l.Each(Query{User: "alice"}, func(e Entry) error { targets = append(targets, e.Target); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 6 || targets[0] != "POST /d0/i0" || targets[5] != "POST /d2/i2" {
		t.Fatalf("%v", targets)
	}
}

func TestNewlinesAndLengthsCannotBreakTheFormat(t *testing.T) {
	l, _, _ := newLog(t, day(2026, 10, 1, 12))
	l.Add(Entry{User: "a\nb", Source: SourcePlugin, Plugin: "p", Action: "command", Target: "x\n{\"fake\":1}\r\n" + strings.Repeat("é", 5000), Detail: strings.Repeat("d", 5000)})
	b, _ := os.ReadFile(filepath.Join(l.Dir(), "audit-2026-10-01.jsonl"))
	if strings.Count(string(b), "\n") != 1 {
		t.Fatalf("a line break got in: %q", b)
	}
	got, _, _ := l.List(Query{})
	if len(got) != 1 || len(got[0].Target) > maxTarget+4 || len(got[0].Detail) > maxDetail+4 {
		t.Fatalf("lengths %d %d", len(got[0].Target), len(got[0].Detail))
	}
}

func TestRedaction(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{HTTPTarget("POST", "/images/load", "quiet=1"), "POST /images/load?quiet=1"},
		{HTTPTarget("POST", "/build", "t=app&buildargs=%7B%7D&X-Registry-Config=abc"), "POST /build?t=app&buildargs=***&X-Registry-Config=***"},
		{HTTPTarget("POST", "/auth", "password=hunter2&username=bob"), "POST /auth?password=***&username=bob"},
		{HTTPTarget("POST", "/x", "token=&a=b"), "POST /x?token=&a=b"},
		{HTTPTarget("POST", "/pull", "fromImage=https://bob:s3cret@reg.example/app"), "POST /pull?fromImage=https://bob:***@reg.example/app"},
		{HTTPTarget("DELETE", "/c/1", ""), "DELETE /c/1"},
		{CommandTarget("docker", []string{"login", "-u", "bob", "--password", "hunter2", "reg.example"}), "docker login -u bob --password *** reg.example"},
		{CommandTarget("tool", []string{"--token=abc", "--name=x"}), "tool --token=*** --name=x"},
		{CommandTarget("run", []string{"API_KEY=zzz", "PATH=/bin", "SECRET_TOKEN=q"}), "run API_KEY=*** PATH=/bin SECRET_TOKEN=***"},
		{CommandTarget("git", []string{"clone", "https://bob:pw@git.example/r.git"}), "git clone https://bob:***@git.example/r.git"},
		{CommandTarget("echo", nil), "echo"},
		{CommandTarget("x", []string{"-p", "8080:80"}), "x -p 8080:80"},
		{RedactURL("https://u:p@h.example/a.tar.gz?access_token=t&x=1"), "https://u:***@h.example/a.tar.gz?access_token=***&x=1"},
	} {
		if c.got != c.want {
			t.Errorf("got %q want %q", c.got, c.want)
		}
	}
	long := CommandTarget("c", []string{strings.Repeat("a", 5000)})
	if len(long) > 400 {
		t.Errorf("an argument is not clipped: %d", len(long))
	}
}

func TestCSV(t *testing.T) {
	code, n := 3, int64(10)
	r := CSVRecord(Entry{Time: day(2026, 10, 1, 1), User: "u", Source: SourcePlugin, Plugin: "p", Action: "command", Target: "=HYPERLINK(\"x\")", Result: Failed, Code: &code, Bytes: &n, Admin: true, Env: "nas", Origin: "via box by ann"})
	if len(r) != len(CSVHeader) || r[0] != "2026-10-01T01:00:00Z" || r[7] != `'=HYPERLINK("x")` || r[9] != "3" || r[10] != "10" || r[11] != "true" || r[13] != "nas" || r[14] != "via box by ann" {
		t.Fatalf("%q", r)
	}
}

// Record writes to the default log, and does nothing without one.
func TestRecordUsesTheDefaultLog(t *testing.T) {
	Record(Entry{User: "nobody", Source: SourceCore, Action: "login"}) // no default: no panic, no file
	l, _, _ := newLog(t, day(2026, 10, 1, 12))
	var logged []string
	l.Errorf = func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
	SetDefault(l)
	defer ClearDefault(l)
	Record(Entry{User: "ann", Source: SourcePlugin, Plugin: "docker", Action: "job", Target: "compose-pull web", Env: "nas", Origin: "via box by bob"})
	got, _, _ := l.List(Query{})
	if len(got) != 1 || got[0].Action != "job" || got[0].Env != "nas" || got[0].Origin != "via box by bob" || got[0].Result != OK {
		t.Fatalf("%+v", got)
	}
	// ClearDefault of another log keeps this one.
	other, _, _ := newLog(t, day(2026, 10, 1, 12))
	ClearDefault(other)
	Record(Entry{User: "ann", Source: SourceCore, Action: "login"})
	if got, _, _ := l.List(Query{}); len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	// A write error goes to Errorf, not to the caller.
	os.RemoveAll(l.Dir())
	os.WriteFile(l.Dir(), []byte("a file where the folder should be"), 0o600)
	l.Close()
	Record(Entry{User: "ann", Source: SourceCore, Action: "login"})
	if len(logged) != 1 || !strings.Contains(logged[0], "activity log") {
		t.Fatalf("errors: %v", logged)
	}
	ClearDefault(l)
	Record(Entry{User: "ann", Source: SourceCore, Action: "login"})
}
