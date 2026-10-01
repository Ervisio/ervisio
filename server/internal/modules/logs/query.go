package logs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
	"github.com/Fonlogen/LinuxAdmin/server/internal/sys"
)

const (
	defaultLimit = 300
	maxLimit     = 2000
	maxSources   = 200
)

type baseParams struct {
	Sources  []string  `json:"sources"`
	Since    int64     `json:"since"`
	Until    int64     `json:"until"`
	Levels   []string  `json:"levels"`
	Text     string    `json:"text"`
	Watchers []Watcher `json:"watchers"`
}

type queryParams struct {
	baseParams
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

// SkippedSource is a source that could not be read.
type SkippedSource struct {
	ID      string `json:"id"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// QueryResult is the answer of logs.query.
type QueryResult struct {
	Entries []Entry         `json:"entries"`
	Next    string          `json:"next,omitempty"`
	HasMore bool            `json:"hasMore"`
	Skipped []SkippedSource `json:"skipped,omitempty"`
}

// watchersOrPrefs returns the watchers sent by the client, or the ones in the
// user's preferences file when the client sent none.
func watchersOrPrefs(w []Watcher) []Watcher {
	if w != nil {
		return w
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, ".config", "linuxadmin", "prefs.json"))
	if err != nil {
		return nil
	}
	var prefs struct {
		Watchers []Watcher `json:"logs.watchers"`
	}
	if json.Unmarshal(b, &prefs) != nil {
		return nil
	}
	return prefs.Watchers
}

func validWatchers(in []Watcher) []Watcher {
	var out []Watcher
	for _, w := range in {
		if checkPath(w.Path) == nil {
			out = append(out, w)
		}
	}
	return out
}

// resolveSpecs parses source ids; "all" expands to the journal, every
// readable file in /var/log and the watchers.
func resolveSpecs(ids []string, watchers []Watcher, admin bool) ([]spec, error) {
	if len(ids) == 0 {
		ids = []string{"all"}
	}
	if len(ids) > maxSources {
		return nil, rpc.Errorf(rpc.Invalid, "too many sources")
	}
	watchers = validWatchers(watchersOrPrefs(watchers))
	var out []spec
	seen := map[string]bool{}
	add := func(s spec) {
		if !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	for _, id := range ids {
		if id == "all" {
			s, _ := parseSourceID("journal", nil)
			add(s)
			for _, lf := range listLogFiles("/var/log", 150) {
				if lf.NeedsAdmin && !admin {
					continue
				}
				if s, err := parseSourceID("file:"+lf.Path, watchers); err == nil {
					add(s)
				}
			}
			for _, w := range watchers {
				if s, err := parseSourceID("file:"+w.Path, watchers); err == nil {
					add(s)
				}
			}
			continue
		}
		s, err := parseSourceID(id, watchers)
		if err != nil {
			return nil, err
		}
		add(s)
	}
	return out, nil
}

var cursorRe = regexp.MustCompile(`^(\d+)\.(\d+)$`)

func (q queryParams) run(ctx context.Context, admin bool) (*QueryResult, error) {
	specs, err := resolveSpecs(q.Sources, q.Watchers, admin)
	if err != nil {
		return nil, err
	}
	f, err := newFilter(q.Since, q.Until, q.Levels, q.Text)
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	until, skip := f.untilUs, 0
	if q.Cursor != "" {
		m := cursorRe.FindStringSubmatch(q.Cursor)
		if m == nil {
			return nil, rpc.Errorf(rpc.Invalid, "invalid cursor")
		}
		t, _ := strconv.ParseInt(m[1], 10, 64)
		skip, _ = strconv.Atoi(m[2])
		if t < until {
			until = t
		}
	}
	if hasJournal(specs) && !admin && !journalReadable() {
		return nil, errNeedsAdmin()
	}
	want := limit + skip + 1
	groups := journalGroups(specs)
	var files []spec
	for _, s := range specs {
		if s.Kind == "file" {
			files = append(files, s)
		}
	}
	type result struct {
		id   string
		ents []Entry
		err  error
	}
	results := make([]result, len(groups)+len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	run := func(i int, id string, fn func() ([]Entry, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ents, err := fn()
			results[i] = result{id, ents, err}
		}()
	}
	for i, g := range groups {
		g := g
		run(i, g.name, func() ([]Entry, error) { return fetchJournal(ctx, g, f, until, want) })
	}
	for i, s := range files {
		s := s
		run(len(groups)+i, s.ID, func() ([]Entry, error) { return fetchFile(ctx, s, f, until, want) })
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	res := &QueryResult{Entries: []Entry{}}
	var all []Entry
	for i, r := range results {
		if r.err != nil {
			if i < len(groups) || len(specs) == 1 {
				return nil, r.err
			}
			e := rpc.ToError(r.err, admin)
			res.Skipped = append(res.Skipped, SkippedSource{ID: r.id, Code: string(e.Code), Message: e.Message})
			continue
		}
		all = append(all, r.ents...)
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].TsUs > all[b].TsUs })
	dropped := 0
	for dropped < skip && dropped < len(all) && all[dropped].TsUs == until {
		dropped++
	}
	all = all[dropped:]
	if len(all) > limit {
		res.HasMore = true
		all = all[:limit]
	}
	res.Entries = append(res.Entries, all...)
	if res.HasMore && len(all) > 0 {
		last := all[len(all)-1].TsUs
		n := 0
		for _, e := range all {
			if e.TsUs == last {
				n++
			}
		}
		if last == until {
			n += skip
		}
		res.Next = strconv.FormatInt(last, 10) + "." + strconv.Itoa(n)
	}
	return res, nil
}

// ---------- histogram ----------

// Bucket counts entries per level in [T, T+step).
type Bucket struct {
	T     int64 `json:"t"`
	Err   int   `json:"err"`
	Warn  int   `json:"warn"`
	Info  int   `json:"info"`
	Debug int   `json:"debug"`
}

func (b *Bucket) add(level string) {
	switch level {
	case LvErr:
		b.Err++
	case LvWarn:
		b.Warn++
	case LvDebug:
		b.Debug++
	default:
		b.Info++
	}
}

// HistogramResult is the answer of logs.histogram. Since, Until and Step are
// in milliseconds.
type HistogramResult struct {
	Since     int64    `json:"since"`
	Until     int64    `json:"until"`
	Step      int64    `json:"step"`
	Buckets   []Bucket `json:"buckets"`
	Truncated bool     `json:"truncated,omitempty"`
}

type histParams struct {
	baseParams
	Buckets int `json:"buckets"`
}

const histJournalCap = 300000

func (h histParams) run(ctx context.Context, admin bool) (*HistogramResult, error) {
	specs, err := resolveSpecs(h.Sources, h.Watchers, admin)
	if err != nil {
		return nil, err
	}
	nb := h.Buckets
	if nb <= 0 {
		nb = 60
	}
	if nb > 500 {
		nb = 500
	}
	now := time.Now().UnixMilli()
	until := h.Until
	if until <= 0 {
		until = now
	}
	since := h.Since
	if since <= 0 || since >= until {
		since = until - 24*3600*1000
	}
	f, err := newFilter(since, until, nil, h.Text)
	if err != nil {
		return nil, err
	}
	if hasJournal(specs) && !admin && !journalReadable() {
		return nil, errNeedsAdmin()
	}
	step := (until - since + int64(nb) - 1) / int64(nb)
	if step < 1 {
		step = 1
	}
	res := &HistogramResult{Since: since, Until: until, Step: step, Buckets: make([]Bucket, nb)}
	for i := range res.Buckets {
		res.Buckets[i].T = since + int64(i)*step
	}
	var mu sync.Mutex
	count := func(tsMs int64, level string) {
		i := int((tsMs - since) / step)
		if tsMs < since || i < 0 {
			return
		}
		if i >= nb {
			i = nb - 1
		}
		mu.Lock()
		res.Buckets[i].add(level)
		mu.Unlock()
	}
	var wg sync.WaitGroup
	var firstErr error
	var emu sync.Mutex
	fail := func(err error) {
		emu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		emu.Unlock()
	}
	groups := journalGroups(specs)
	sem := make(chan struct{}, 6)
	spawn := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fn()
		}()
	}
	for _, g := range groups {
		g := g
		spawn(func() {
			args := journalArgs(g, f, f.untilUs, "-r", "-n", strconv.Itoa(histJournalCap), "--output-fields=PRIORITY")
			n := 0
			err := sys.Stream(ctx, "journalctl", args, func(line string) error {
				var j struct {
					T jstr `json:"__REALTIME_TIMESTAMP"`
					P jstr `json:"PRIORITY"`
				}
				if json.Unmarshal([]byte(line), &j) != nil {
					return nil
				}
				us, err := strconv.ParseInt(string(j.T), 10, 64)
				if err != nil {
					return nil
				}
				lvl := LvInfo
				if p, err := strconv.Atoi(string(j.P)); err == nil {
					lvl = levelFromPriority(p)
				}
				count(us/1000, lvl)
				n++
				return nil
			})
			if n >= histJournalCap {
				res.Truncated = true
			}
			if err = journalErr(err); err != nil {
				fail(err)
			}
		})
	}
	for _, s := range specs {
		if s.Kind != "file" {
			continue
		}
		s := s
		spawn(func() {
			err := scanFile(ctx, s.Path, s.Label, s.Format, maxScanBytes, f.sinceUs, func(e *Entry) bool {
				if e.TsUs <= f.untilUs && f.textOK(e.Message) {
					count(e.Ts, e.Level)
				}
				return true
			})
			if err != nil && len(specs) == 1 {
				fail(err)
			}
		})
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

// ---------- context ----------

type contextParams struct {
	Source   string    `json:"source"`
	Cursor   string    `json:"cursor"`
	File     string    `json:"file"`
	Line     int       `json:"line"`
	Before   int       `json:"before"`
	After    int       `json:"after"`
	Watchers []Watcher `json:"watchers"`
}

// ContextResult holds the lines around one entry, oldest first; Index points
// at the entry itself.
type ContextResult struct {
	Entries []Entry `json:"entries"`
	Index   int     `json:"index"`
}

var journalCursorRe = regexp.MustCompile(`^[A-Za-z0-9=;_-]{1,300}$`)

func (p contextParams) run(ctx context.Context, admin bool) (*ContextResult, error) {
	before, after := p.Before, p.After
	if before < 0 || before > 200 || after < 0 || after > 200 {
		return nil, rpc.Errorf(rpc.Invalid, "before and after must be between 0 and 200")
	}
	if before == 0 && after == 0 {
		before, after = 5, 5
	}
	file, line := p.File, p.Line
	if rest, ok := strings.CutPrefix(p.Cursor, "file:"); ok {
		i := strings.LastIndexByte(rest, ':')
		if i <= 0 {
			return nil, rpc.Errorf(rpc.Invalid, "invalid cursor")
		}
		file = rest[:i]
		line, _ = strconv.Atoi(rest[i+1:])
	}
	if file != "" {
		return fileContext(ctx, file, line, before, after, watchersOrPrefs(p.Watchers))
	}
	if !journalCursorRe.MatchString(p.Cursor) {
		return nil, rpc.Errorf(rpc.Invalid, "give a journal cursor or a file and line")
	}
	var specs []spec
	if p.Source != "" && p.Source != "all" {
		s, err := parseSourceID(p.Source, nil)
		if err != nil {
			return nil, err
		}
		if s.Kind != "file" {
			specs = append(specs, s)
		}
	}
	if len(specs) == 0 {
		specs = []spec{{ID: "journal", Kind: "journal"}}
	}
	if !admin && !journalReadable() {
		return nil, errNeedsAdmin()
	}
	g := journalGroups(specs)[0]
	run := func(extra ...string) ([]Entry, error) {
		args := append([]string{"-o", "json", "--no-pager", "-q", "--cursor=" + p.Cursor}, g.args...)
		args = append(args, extra...)
		var out []Entry
		err := sys.Stream(ctx, "journalctl", args, func(l string) error {
			if e, ok := parseJournalLine(l); ok {
				out = append(out, *e)
			}
			return nil
		})
		return out, journalErr(err)
	}
	older, err := run("-r", "-n", strconv.Itoa(before+1))
	if err != nil {
		return nil, err
	}
	newer, err := run("-n", strconv.Itoa(after+1))
	if err != nil {
		return nil, err
	}
	// older is newest-first and starts at the entry itself; newer starts there too.
	res := &ContextResult{}
	for i := len(older) - 1; i >= 1; i-- {
		res.Entries = append(res.Entries, older[i])
	}
	res.Index = len(res.Entries)
	if len(newer) > 0 {
		res.Entries = append(res.Entries, newer...)
	} else if len(older) > 0 {
		res.Entries = append(res.Entries, older[0])
	}
	if res.Entries == nil {
		res.Entries = []Entry{}
	}
	return res, nil
}

// fileContext reads lines [line-before, line+after] of a file.
func fileContext(ctx context.Context, path string, line, before, after int, watchers []Watcher) (*ContextResult, error) {
	s, err := parseSourceID("file:"+path, validWatchers(watchers))
	if err != nil {
		return nil, err
	}
	if line < 1 {
		return nil, rpc.Errorf(rpc.Invalid, "line numbers are not available for this file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	from, to := line-before, line+after
	if from < 1 {
		from = 1
	}
	st, _ := f.Stat()
	if st != nil && st.Size() > maxLineCountSize {
		return nil, rpc.Errorf(rpc.Unavailable, "this file is too large to show surrounding lines")
	}
	res := &ContextResult{Entries: []Entry{}}
	now, loc := time.Now(), time.Local
	no := 0
	var last lineState
	err = forwardLines(ctx, f, func(text string) bool {
		no++
		if no < from {
			return true
		}
		if no > to {
			return false
		}
		text = strings.TrimRight(text, "\r")
		e, hasTs := lineEntry(text, s.Label, path, no, s.Format, now, loc)
		if hasTs {
			last = lineState{e.TsUs, e.Level}
		} else {
			e.TsUs, e.Ts = last.ts, last.ts/1000
			if e.Level == "" {
				e.Level = last.lvl
			}
		}
		if e.Level == "" {
			e.Level = LvInfo
		}
		if no == line {
			res.Index = len(res.Entries)
		}
		res.Entries = append(res.Entries, *e)
		return true
	})
	if err != nil {
		return nil, err
	}
	if len(res.Entries) == 0 {
		return nil, rpc.Errorf(rpc.NotFound, "line %d is not in %s (the file may have been rotated)", line, path)
	}
	return res, nil
}
