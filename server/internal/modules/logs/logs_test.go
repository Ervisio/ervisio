package logs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleJournalLine = `{"__CURSOR":"s=1c8a;i=30ea1;b=6f5a;m=1c5fe8dc8;t=65ccb9a5e629a;x=1dfa","__REALTIME_TIMESTAMP":"1790879398257306","PRIORITY":"6","MESSAGE":"pam_unix(sudo:session): session opened for user root(uid=0)","SYSLOG_IDENTIFIER":"sudo","_SYSTEMD_UNIT":"user@1000.service","_PID":"219929","_TRANSPORT":"syslog"}`

func TestParseJournalLine(t *testing.T) {
	e, ok := parseJournalLine(sampleJournalLine)
	if !ok {
		t.Fatal("not parsed")
	}
	if e.TsUs != 1790879398257306 || e.Ts != 1790879398257 {
		t.Errorf("ts %d %d", e.TsUs, e.Ts)
	}
	if e.Level != LvInfo || e.Source != "sudo" || e.Unit != "user@1000.service" || e.Pid != 219929 {
		t.Errorf("got %+v", e)
	}
	if !strings.HasPrefix(e.Cursor, "s=1c8a;") {
		t.Errorf("cursor %q", e.Cursor)
	}
	// kernel, error priority, binary MESSAGE as byte array
	e, ok = parseJournalLine(`{"__CURSOR":"c","__REALTIME_TIMESTAMP":"5000000","PRIORITY":"3","_TRANSPORT":"kernel","MESSAGE":[104,105,10]}`)
	if !ok || e.Source != "kernel" || e.Level != LvErr || e.Message != "hi" {
		t.Errorf("got %+v ok=%v", e, ok)
	}
	if _, ok := parseJournalLine("not json"); ok {
		t.Error("garbage accepted")
	}
}

func TestPriorityLevels(t *testing.T) {
	want := map[int]string{0: LvErr, 3: LvErr, 4: LvWarn, 5: LvInfo, 6: LvInfo, 7: LvDebug}
	for p, l := range want {
		if got := levelFromPriority(p); got != l {
			t.Errorf("priority %d: %s, want %s", p, got, l)
		}
	}
	lo, hi, all := priorityRange(map[string]bool{LvErr: true, LvWarn: true})
	if lo != 0 || hi != 4 || all {
		t.Errorf("range %d..%d %v", lo, hi, all)
	}
	if _, _, all := priorityRange(nil); !all {
		t.Error("nil levels should mean all")
	}
}

func TestDetectLevel(t *testing.T) {
	cases := map[string]string{
		"2026/10/01 [error] 12#12: *1 upstream timed out": LvErr,
		"[ALPM] warning: dependency cycle detected":       LvWarn,
		"FATAL: password authentication failed":           LvErr,
		"Critical: disk almost full":                      LvErr,
		"level=debug msg=hello":                           LvDebug,
		"opening /var/log/error.log for reading":          "",
		"GET / HTTP/1.1 200":                              "",
		"ERR: nope":                                       LvErr,
		"things went fine, no errors":                     "",
	}
	for in, want := range cases {
		if got := detectLevel(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestParseTime(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 10, 1, 21, 0, 0, 0, loc)
	cases := []struct {
		line string
		want time.Time
		rest string
	}{
		{"2026-10-01T20:29:58.123+02:00 hello", time.Date(2026, 10, 1, 18, 29, 58, 123e6, loc), "hello"},
		{"2026-10-01 20:29:58,500 INFO started", time.Date(2026, 10, 1, 20, 29, 58, 500e6, loc), "INFO started"},
		{"[2026-10-01T20:29:58+0200] [ALPM] upgraded x", time.Date(2026, 10, 1, 18, 29, 58, 0, loc), "[ALPM] upgraded x"},
		{"2026/10/01 20:29:58 [error] 1#1: boom", time.Date(2026, 10, 1, 20, 29, 58, 0, loc), "[error] 1#1: boom"},
		{"Oct  1 20:29:58 arch sudo[1]: x", time.Date(2026, 10, 1, 20, 29, 58, 0, loc), "arch sudo[1]: x"},
		{"Dec 31 23:59:59 arch x", time.Date(2025, 12, 31, 23, 59, 59, 0, loc), "arch x"},
		{`192.168.1.2 - - [01/Oct/2026:20:29:58 +0200] "GET / HTTP/1.1" 200 5`, time.Date(2026, 10, 1, 18, 29, 58, 0, loc), `192.168.1.2 - - [01/Oct/2026:20:29:58 +0200] "GET / HTTP/1.1" 200 5`},
	}
	for _, c := range cases {
		got, rest, ok := parseTime(c.line, now, loc)
		if !ok || !got.Equal(c.want) || rest != c.rest {
			t.Errorf("%q: %v %q %v, want %v %q", c.line, got, rest, ok, c.want, c.rest)
		}
	}
	if _, _, ok := parseTime("    at Foo.bar (x.js:1:1)", now, loc); ok {
		t.Error("stack line has no timestamp")
	}
}

func TestJSONLine(t *testing.T) {
	now := time.Now()
	msg, lvl, ts, ok := parseJSONLine(`{"level":"error","msg":"db down","time":"2026-10-01T10:00:00Z"}`, now, time.UTC)
	if !ok || msg != "db down" || lvl != LvErr || ts.Unix() != 1790848800 {
		t.Errorf("%q %q %v %v", msg, lvl, ts, ok)
	}
	_, lvl, ts, _ = parseJSONLine(`{"severity":"WARNING","message":"x","ts":1790848800.5}`, now, time.UTC)
	if lvl != LvWarn || ts.Unix() != 1790848800 {
		t.Errorf("%q %v", lvl, ts)
	}
	_, lvl, ts, _ = parseJSONLine(`{"level":50,"msg":"x","time":1790848800000}`, now, time.UTC)
	if lvl != LvErr || ts.Unix() != 1790848800 {
		t.Errorf("%q %v", lvl, ts)
	}
	if _, _, _, ok := parseJSONLine("plain text", now, time.UTC); ok {
		t.Error("plain text accepted")
	}
}

func writeLog(t *testing.T, lines []string, trailingNL bool) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.log")
	data := strings.Join(lines, "\n")
	if trailingNL {
		data += "\n"
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanFileBackward(t *testing.T) {
	lines := []string{
		"2026-10-01 10:00:00 [info] start",
		"2026-10-01 10:00:05 [error] crash",
		"  at foo (a.js:1)",
		"  at bar (b.js:2)",
		"2026-10-01 10:00:09 [warn] slow",
		"",
		"2026-10-01 10:00:10 [debug] tick",
	}
	for _, nl := range []bool{true, false} {
		p := writeLog(t, lines, nl)
		var got []Entry
		err := scanFile(context.Background(), p, "app", "auto", 0, 0, func(e *Entry) bool { got = append(got, *e); return true })
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 6 {
			t.Fatalf("nl=%v: %d entries", nl, len(got))
		}
		want := []struct {
			lvl  string
			line int
		}{{LvDebug, 7}, {LvWarn, 5}, {LvErr, 4}, {LvErr, 3}, {LvErr, 2}, {LvInfo, 1}}
		for i, w := range want {
			if got[i].Level != w.lvl || got[i].Line != w.line {
				t.Errorf("nl=%v entry %d: %s line %d, want %s line %d (%q)", nl, i, got[i].Level, got[i].Line, w.lvl, w.line, got[i].Message)
			}
		}
		if got[2].Ts != got[3].Ts || got[2].Ts != got[4].Ts {
			t.Error("continuation lines should share the header timestamp")
		}
	}
}

func TestScanFileSince(t *testing.T) {
	p := writeLog(t, []string{
		"2026-10-01 10:00:00 [info] old",
		"2026-10-01 11:00:00 [info] mid",
		"2026-10-01 12:00:00 [info] new",
	}, true)
	since := time.Date(2026, 10, 1, 10, 30, 0, 0, time.Local).UnixMicro()
	var n int
	_ = scanFile(context.Background(), p, "a", "auto", 0, since, func(e *Entry) bool { n++; return true })
	if n != 2 {
		t.Errorf("got %d entries since 10:30", n)
	}
}

func TestScanFileLargeBlocks(t *testing.T) {
	var lines []string
	for i := 0; i < 20000; i++ {
		lines = append(lines, fmt.Sprintf("2026-10-01 10:%02d:%02d [info] line number %d with some padding to cross blocks", (i/60)%60, i%60, i))
	}
	p := writeLog(t, lines, true)
	i := len(lines)
	err := scanFile(context.Background(), p, "a", "auto", 0, 0, func(e *Entry) bool {
		i--
		if e.Line != i+1 || !strings.HasSuffix(e.Message, fmt.Sprintf("line number %d with some padding to cross blocks", i)) {
			t.Fatalf("line %d: got line %d %q", i+1, e.Line, e.Message)
		}
		return true
	})
	if err != nil || i != 0 {
		t.Fatalf("err=%v remaining=%d", err, i)
	}
}

func TestQueryPagingFile(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		// five lines per second: paging must not lose or repeat lines
		lines = append(lines, fmt.Sprintf("2026-10-01 10:00:%02d [info] msg %d", i/5, i))
	}
	p := writeLog(t, lines, true)
	q := queryParams{baseParams: baseParams{Sources: []string{"file:" + p}, Watchers: []Watcher{}}, Limit: 7}
	seen := map[string]bool{}
	var order []string
	for page := 0; page < 20; page++ {
		res, err := q.run(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range res.Entries {
			if seen[e.Message] {
				t.Fatalf("duplicate %q on page %d", e.Message, page)
			}
			seen[e.Message] = true
			order = append(order, e.Message)
		}
		if !res.HasMore {
			break
		}
		q.Cursor = res.Next
	}
	if len(seen) != 50 {
		t.Fatalf("saw %d of 50 lines", len(seen))
	}
	if order[0] != "[info] msg 49" || order[49] != "[info] msg 0" {
		t.Errorf("order %v ... %v", order[0], order[49])
	}
}

func TestQueryFilters(t *testing.T) {
	p := writeLog(t, []string{
		"2026-10-01 10:00:00 [info] hello world",
		"2026-10-01 10:00:01 [error] Disk FAILURE",
		"2026-10-01 10:00:02 [warn] almost full",
	}, true)
	run := func(levels []string, text string) []Entry {
		q := queryParams{baseParams: baseParams{Sources: []string{"file:" + p}, Levels: levels, Text: text, Watchers: []Watcher{}}}
		res, err := q.run(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		return res.Entries
	}
	if got := run([]string{"err"}, ""); len(got) != 1 || got[0].Level != LvErr {
		t.Errorf("err filter: %+v", got)
	}
	if got := run(nil, "disk failure"); len(got) != 1 {
		t.Errorf("text filter: %+v", got)
	}
	if got := run([]string{"err", "info"}, ""); len(got) != 2 {
		t.Errorf("err+info: %d", len(got))
	}
	q := queryParams{baseParams: baseParams{Sources: []string{"bogus"}}}
	if _, err := q.run(context.Background(), false); err == nil {
		t.Error("bogus source accepted")
	}
	q = queryParams{baseParams: baseParams{Sources: []string{"file:../etc/passwd"}}}
	if _, err := q.run(context.Background(), false); err == nil {
		t.Error("relative path accepted")
	}
	q = queryParams{baseParams: baseParams{Sources: []string{"unit:--help"}}}
	if _, err := q.run(context.Background(), false); err == nil {
		t.Error("option-like unit accepted")
	}
}

func TestHistogramAndContext(t *testing.T) {
	p := writeLog(t, []string{
		"2026-10-01 10:00:00 [info] a",
		"2026-10-01 10:30:00 [error] b",
		"2026-10-01 10:30:01 [warn] c",
		"2026-10-01 11:00:00 [info] d",
	}, true)
	start := time.Date(2026, 10, 1, 10, 0, 0, 0, time.Local).UnixMilli()
	h := histParams{baseParams: baseParams{Sources: []string{"file:" + p}, Since: start, Until: start + 3600*1000, Watchers: []Watcher{}}, Buckets: 2}
	res, err := h.run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Buckets) != 2 || res.Buckets[0].Info != 1 || res.Buckets[1].Err != 1 || res.Buckets[1].Warn != 1 || res.Buckets[1].Info != 1 {
		t.Errorf("buckets %+v", res.Buckets)
	}
	cp := contextParams{File: p, Line: 3, Before: 1, After: 5, Watchers: []Watcher{}}
	cr, err := cp.run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cr.Entries) != 3 || cr.Index != 1 || cr.Entries[1].Message != "[warn] c" {
		t.Errorf("context %+v", cr)
	}
	cp = contextParams{Cursor: "file:" + p + ":2", Before: 1, After: 1, Watchers: []Watcher{}}
	if cr, err = cp.run(context.Background(), false); err != nil || cr.Entries[cr.Index].Message != "[error] b" {
		t.Errorf("cursor context %+v %v", cr, err)
	}
}

func TestListLogFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pacman.log"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "pacman.log.1"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "old.log-20240101"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "wtmp"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "bin.log"), []byte("a\x00b"), 0o644)
	os.MkdirAll(filepath.Join(dir, "nginx"), 0o755)
	os.WriteFile(filepath.Join(dir, "nginx", "error.log"), []byte("x\n"), 0o644)
	var names []string
	for _, f := range listLogFiles(dir, 50) {
		names = append(names, strings.TrimPrefix(f.Path, dir+"/"))
	}
	if strings.Join(names, ",") != "nginx/error.log,pacman.log" {
		t.Errorf("got %v", names)
	}
}

func TestJournalArgs(t *testing.T) {
	f, _ := newFilter(1000, 2000, []string{"err", "warn"}, "time.out")
	args := strings.Join(journalArgs(jgroup{args: []string{"--unit=nginx.service"}}, f, f.untilUs, "-r"), " ")
	for _, want := range []string{"--unit=nginx.service", "--since=@1.000000", "--until=@2.000999", "-p 0..4", `--grep=time\.out`, "-r"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in %s", want, args)
		}
	}
	gs := journalGroups([]spec{{Kind: "unit", Unit: "a.service"}, {Kind: "unit", Unit: "b.service"}, {Kind: "kernel"}})
	if len(gs) != 2 || len(gs[0].args) != 2 {
		t.Errorf("groups %+v", gs)
	}
	if gs := journalGroups([]spec{{Kind: "journal"}, {Kind: "kernel"}}); len(gs) != 1 {
		t.Errorf("journal should cover kernel: %+v", gs)
	}
}

func TestFollowFile(t *testing.T) {
	p := writeLog(t, []string{"2026-10-01 10:00:00 [info] before"}, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Entry, 10)
	go followFile(ctx, spec{ID: "file:" + p, Kind: "file", Path: p, Label: "app", Format: "auto"}, filter{untilUs: noLimit}, out)
	time.Sleep(600 * time.Millisecond)
	fh, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString("2026-10-01 10:00:01 [error] after\n  trace line\n")
	fh.Close()
	var got []Entry
	deadline := time.After(3 * time.Second)
	for len(got) < 2 {
		select {
		case e := <-out:
			got = append(got, e)
		case <-deadline:
			t.Fatalf("got %d entries", len(got))
		}
	}
	if got[0].Level != LvErr || got[1].Level != LvErr || got[1].Message != "  trace line" {
		t.Errorf("%+v", got)
	}
	// rotation: replace the file
	os.Rename(p, p+".1")
	os.WriteFile(p, []byte("2026-10-01 10:05:00 [warn] fresh\n"), 0o644)
	select {
	case e := <-out:
		if e.Message != "[warn] fresh" {
			t.Errorf("after rotation: %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no entry after rotation")
	}
}
