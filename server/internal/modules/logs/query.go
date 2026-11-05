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

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
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
	b, err := os.ReadFile(filepath.Join(home, brand.UserDataDir, brand.PrefsFile))
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
			addAllSources(watchers, admin, add)
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

// addAllSources feeds add with the sources "all" stands for.
func addAllSources(watchers []Watcher, admin bool, add func(spec)) {
	var logFiles []LogFile
	if onWindows {
		for _, ch := range eventChannels {
			s, _ := parseSourceID("evt:"+ch, nil)
			add(s)
		}
	} else {
		s, _ := parseSourceID("journal", nil)
		add(s)
		logFiles = listLogFiles("/var/log", 150)
	}
	for _, lf := range logFiles {
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
}

var cursorRe = regexp.MustCompile(`^(\d+)\.(\d+)$`)

// fetchConcurrency is how many sources are read at the same time.
const fetchConcurrency = 6

func (q queryParams) run(ctx context.Context, admin bool) (*QueryResult, error) {
	specs, err := resolveSpecs(q.Sources, q.Watchers, admin)
	if err != nil {
		return nil, err
	}
	f, err := newFilter(q.Since, q.Until, q.Levels, q.Text)
	if err != nil {
		return nil, err
	}
	limit := clampOrDefault(q.Limit, defaultLimit, maxLimit)
	until, skip, err := parseCursor(q.Cursor, f.untilUs)
	if err != nil {
		return nil, err
	}
	if hasJournal(specs) && !admin && !journalReadable() {
		return nil, errNeedsAdmin()
	}
	groups := journalGroups(specs)
	results := fetchSources(ctx, groups, specs, f, until, limit+skip+1)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	all, skipped, err := mergeResults(results, len(groups), len(specs), admin)
	if err != nil {
		return nil, err
	}
	res := &QueryResult{Entries: []Entry{}, Skipped: skipped}
	page := dropSeen(all, until, skip)
	if len(page) > limit {
		res.HasMore = true
		page = page[:limit]
	}
	res.Entries = append(res.Entries, page...)
	if res.HasMore && len(page) > 0 {
		res.Next = nextCursor(page, until, skip)
	}
	return res, nil
}

// clampOrDefault returns def when v is not positive, and at most ceiling.
func clampOrDefault(v, def, ceiling int) int {
	if v <= 0 {
		v = def
	}
	if v > ceiling {
		v = ceiling
	}
	return v
}

// parseCursor decodes "<untilUs>.<skip>": the page ends at the cursor time,
// and skip entries at exactly that time were already returned.
func parseCursor(cursor string, until int64) (int64, int, error) {
	if cursor == "" {
		return until, 0, nil
	}
	m := cursorRe.FindStringSubmatch(cursor)
	if m == nil {
		return 0, 0, rpc.Errorf(rpc.Invalid, "invalid cursor")
	}
	t, _ := strconv.ParseInt(m[1], 10, 64)
	skip, _ := strconv.Atoi(m[2])
	if t < until {
		until = t
	}
	return until, skip, nil
}

func specsOfKind(specs []spec, kind string) []spec {
	var out []spec
	for _, s := range specs {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

// runBounded runs the tasks concurrently, at most fetchConcurrency at a
// time, and waits for all of them.
func runBounded(tasks []func()) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, fetchConcurrency)
	for _, task := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			task()
		}()
	}
	wg.Wait()
}

type sourceResult struct {
	id   string
	ents []Entry
	err  error
}

// fetchSources reads the journal groups, then the files, then the event
// channels; the results keep that order.
func fetchSources(ctx context.Context, groups []jgroup, specs []spec, f filter, until int64, want int) []sourceResult {
	files, evts := specsOfKind(specs, "file"), specsOfKind(specs, "evt")
	results := make([]sourceResult, len(groups)+len(files)+len(evts))
	tasks := make([]func(), 0, len(results))
	add := func(id string, fetch func() ([]Entry, error)) {
		i := len(tasks)
		tasks = append(tasks, func() {
			ents, err := fetch()
			results[i] = sourceResult{id, ents, err}
		})
	}
	for _, g := range groups {
		add(g.name, func() ([]Entry, error) { return fetchJournal(ctx, g, f, until, want) })
	}
	for _, s := range files {
		add(s.ID, func() ([]Entry, error) { return fetchFile(ctx, s, f, until, want) })
	}
	for _, s := range evts {
		add(s.ID, func() ([]Entry, error) { return fetchEventLog(ctx, s.Channel, f, until, want) })
	}
	runBounded(tasks)
	return results
}

// mergeResults joins the entries of every source, newest first. A failing
// journal group, or a failing only source, fails the query; other failing
// sources are reported as skipped.
func mergeResults(results []sourceResult, groupCount, specCount int, admin bool) ([]Entry, []SkippedSource, error) {
	var all []Entry
	var skipped []SkippedSource
	for i, r := range results {
		if r.err != nil {
			if i < groupCount || specCount == 1 {
				return nil, nil, r.err
			}
			e := rpc.ToError(r.err, admin)
			skipped = append(skipped, SkippedSource{ID: r.id, Code: string(e.Code), Message: e.Message})
			continue
		}
		all = append(all, r.ents...)
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].TsUs > all[b].TsUs })
	return all, skipped, nil
}

// dropSeen drops the entries at the cursor time that the previous page
// already returned.
func dropSeen(all []Entry, until int64, skip int) []Entry {
	dropped := 0
	for dropped < skip && dropped < len(all) && all[dropped].TsUs == until {
		dropped++
	}
	return all[dropped:]
}

// nextCursor points after page: its last time and how many entries at that
// time have been returned so far.
func nextCursor(page []Entry, until int64, skip int) string {
	last := page[len(page)-1].TsUs
	n := 0
	for _, e := range page {
		if e.TsUs == last {
			n++
		}
	}
	if last == until {
		n += skip
	}
	return strconv.FormatInt(last, 10) + "." + strconv.Itoa(n)
}

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

const (
	histJournalCap    = 300000
	defaultHistBucket = 60
	maxHistBuckets    = 500
	defaultHistSpanMs = 24 * 3600 * 1000
)

func (h histParams) run(ctx context.Context, admin bool) (*HistogramResult, error) {
	specs, err := resolveSpecs(h.Sources, h.Watchers, admin)
	if err != nil {
		return nil, err
	}
	nb := clampOrDefault(h.Buckets, defaultHistBucket, maxHistBuckets)
	since, until := histWindow(h.Since, h.Until)
	f, err := newFilter(since, until, nil, h.Text)
	if err != nil {
		return nil, err
	}
	if hasJournal(specs) && !admin && !journalReadable() {
		return nil, errNeedsAdmin()
	}
	c := newHistCounter(since, until, nb)
	runBounded(c.tasks(ctx, specs, f))
	if c.firstErr != nil {
		return nil, c.firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.res, nil
}

// histWindow defaults until to now and since to one day before until.
func histWindow(since, until int64) (int64, int64) {
	if until <= 0 {
		until = time.Now().UnixMilli()
	}
	if since <= 0 || since >= until {
		since = until - defaultHistSpanMs
	}
	return since, until
}

// histCounter fills the buckets of a histogram from sources read
// concurrently.
type histCounter struct {
	mu       sync.Mutex
	res      *HistogramResult
	firstErr error
}

func newHistCounter(since, until int64, nb int) *histCounter {
	step := (until - since + int64(nb) - 1) / int64(nb)
	if step < 1 {
		step = 1
	}
	res := &HistogramResult{Since: since, Until: until, Step: step, Buckets: make([]Bucket, nb)}
	for i := range res.Buckets {
		res.Buckets[i].T = since + int64(i)*step
	}
	return &histCounter{res: res}
}

func (c *histCounter) count(tsMs int64, level string) {
	i := int((tsMs - c.res.Since) / c.res.Step)
	if tsMs < c.res.Since || i < 0 {
		return
	}
	if nb := len(c.res.Buckets); i >= nb {
		i = nb - 1
	}
	c.mu.Lock()
	c.res.Buckets[i].add(level)
	c.mu.Unlock()
}

func (c *histCounter) fail(err error) {
	c.mu.Lock()
	if c.firstErr == nil {
		c.firstErr = err
	}
	c.mu.Unlock()
}

func (c *histCounter) markTruncated() {
	c.mu.Lock()
	c.res.Truncated = true
	c.mu.Unlock()
}

func (c *histCounter) tasks(ctx context.Context, specs []spec, f filter) []func() {
	var tasks []func()
	for _, g := range journalGroups(specs) {
		tasks = append(tasks, func() { c.countJournal(ctx, g, f) })
	}
	onlySource := len(specs) == 1
	for _, s := range specsOfKind(specs, "file") {
		tasks = append(tasks, func() { c.countFile(ctx, s, f, onlySource) })
	}
	for _, s := range specsOfKind(specs, "evt") {
		tasks = append(tasks, func() { c.countEventLog(ctx, s, f) })
	}
	return tasks
}

func (c *histCounter) countJournal(ctx context.Context, g jgroup, f filter) {
	args := journalArgs(g, f, f.untilUs, "-r", "-n", strconv.Itoa(histJournalCap), "--output-fields=PRIORITY")
	n := 0
	err := sys.Stream(ctx, "journalctl", args, func(line string) error {
		if tsMs, level, ok := parseHistLine(line); ok {
			c.count(tsMs, level)
			n++
		}
		return nil
	})
	if n >= histJournalCap {
		c.markTruncated()
	}
	if err = journalErr(err); err != nil {
		c.fail(err)
	}
}

// parseHistLine reads the time (ms) and level of a journalctl JSON line.
func parseHistLine(line string) (int64, string, bool) {
	var j struct {
		T jstr `json:"__REALTIME_TIMESTAMP"`
		P jstr `json:"PRIORITY"`
	}
	if json.Unmarshal([]byte(line), &j) != nil {
		return 0, "", false
	}
	us, err := strconv.ParseInt(string(j.T), 10, 64)
	if err != nil {
		return 0, "", false
	}
	level := LvInfo
	if p, err := strconv.Atoi(string(j.P)); err == nil {
		level = levelFromPriority(p)
	}
	return us / 1000, level, true
}

// countFile counts a file; a file that cannot be read fails the histogram
// only when it is the only source.
func (c *histCounter) countFile(ctx context.Context, s spec, f filter, onlySource bool) {
	err := scanFile(ctx, s.Path, s.Label, s.Format, maxScanBytes, f.sinceUs, func(e *Entry) bool {
		if e.TsUs <= f.untilUs && f.textOK(e.Message) {
			c.count(e.Ts, e.Level)
		}
		return true
	})
	if err != nil && onlySource {
		c.fail(err)
	}
}

func (c *histCounter) countEventLog(ctx context.Context, s spec, f filter) {
	ents, err := fetchEventLog(ctx, s.Channel, filter{sinceUs: f.sinceUs, untilUs: f.untilUs, text: f.text}, f.untilUs, histEventCap)
	if len(ents) >= histEventCap {
		c.markTruncated()
	}
	for i := range ents {
		c.count(ents[i].Ts, ents[i].Level)
	}
	if err != nil {
		c.fail(err)
	}
}

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

// inheritLineState gives a line without a timestamp the time and level of
// the line before it, and remembers a line that has one.
func inheritLineState(e *Entry, hasTs bool, last *lineState) {
	if hasTs {
		*last = lineState{e.TsUs, e.Level}
	} else {
		e.TsUs, e.Ts = last.ts, last.ts/1000
		if e.Level == "" {
			e.Level = last.lvl
		}
	}
	if e.Level == "" {
		e.Level = LvInfo
	}
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
	defer func() { _ = f.Close() }()
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
		inheritLineState(e, hasTs, &last)
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
