// Package audit is the activity log: an append-only record of what users and
// plugins changed through the console (sign-ins, administrator unlocks,
// plugin installs, settings changes, and every mutating plugin call).
//
// The daemon (root) writes JSON lines to one file per UTC day,
// <dir>/audit-YYYY-MM-DD.jsonl, readable only by its owner. Old files are
// deleted after the retention period. Secrets are never stored: see
// Redact.
package audit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Result values of Entry.Result.
const (
	// OK: the call ran and succeeded (exit code 0, HTTP status below 400).
	OK = "ok"
	// Failed: the call ran but the command exited non-zero or the service
	// answered with an HTTP status of 400 or more.
	Failed = "failed"
	// Denied: refused by the rules or the user's rights.
	Denied = "denied"
	// Error: it could not be run (the service is down, bad request...).
	Error = "error"
)

// Sources of Entry.Source.
const (
	SourcePlugin = "plugin"
	SourceCore   = "core"
)

// Limits.
const (
	maxTarget = 1024
	maxDetail = 300
	maxArg    = 256
	// DefaultLimit and MaxLimit bound List.
	DefaultLimit = 100
	MaxLimit     = 1000
	filePrefix   = "audit-"
	fileSuffix   = ".jsonl"
	dayLayout    = "2006-01-02"
	maxLine      = 1 << 20
)

// Entry is one line of the log.
type Entry struct {
	Time   time.Time `json:"time"`
	User   string    `json:"user"`
	IP     string    `json:"ip,omitempty"`
	Source string    `json:"source"`
	Plugin string    `json:"plugin,omitempty"`
	// Action is "command", "pty", "http", "upload", "download", "file.write",
	// "file.mkdir", "file.remove" for plugins; "login", "login.failed",
	// "logout", "unlock", "lock", "plugin.install", "plugin.uninstall",
	// "plugin.enable", "plugin.disable", "settings" for the core.
	Action string `json:"action"`
	// Via is the HTTP API a request went to (capabilities.http name).
	Via string `json:"via,omitempty"`
	// Target is "METHOD /path?query" or "command arg arg", a file path, or
	// the setting changed. Secrets are removed (see Redact).
	Target string `json:"target,omitempty"`
	Result string `json:"result"`
	// Code is the exit code or HTTP status.
	Code *int `json:"code,omitempty"`
	// Bytes is the size transferred (uploads, downloads, file writes).
	Bytes *int64 `json:"bytes,omitempty"`
	// Admin is set when the call ran with administrator rights.
	Admin  bool   `json:"admin,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Env is the environment (endpoint) the call went to, when it was not
	// this machine.
	Env string `json:"env,omitempty"`
	// Origin says that another Ervisio server proxied the call, as
	// "via <server> by <user>"; User is then the account on this machine.
	Origin string `json:"origin,omitempty"`
}

// The default log lets code outside the server package (background jobs,
// webhooks) record an action without holding the Server:
//
//	audit.Record(audit.Entry{User: "ann", Source: audit.SourcePlugin, Plugin: "docker", Action: "job",
//		Target: "compose-pull web", Result: audit.OK})
//
// Source is plugin for what a plugin does (a job runs as its owner: User is that
// account, IP is empty) and core for the console's own actions. The daemon sets
// the default when it starts serving; before that, and in tools and tests,
// Record does nothing.
var defaultLog atomic.Pointer[Log]

// SetDefault makes l the log Record writes to (nil removes it).
func SetDefault(l *Log) { defaultLog.Store(l) }

// ClearDefault removes l as the default log if it still is.
func ClearDefault(l *Log) { defaultLog.CompareAndSwap(l, nil) }

// Record appends e to the default log. It reports nothing back: what is
// logged never decides what a caller does. Add errors go to Errorf when set.
func Record(e Entry) {
	l := defaultLog.Load()
	if l == nil {
		return
	}
	if err := l.Add(e); err != nil && l.Errorf != nil {
		l.Errorf("activity log: %v", err)
	}
}

// Config is read on every write, so a settings change applies at once.
type Config interface {
	// AuditEnabled reports whether entries are recorded.
	AuditEnabled() bool
	// AuditRetentionDays is how long files are kept (0 = forever).
	AuditRetentionDays() int
}

// Log writes and reads the activity log of one state folder.
type Log struct {
	dir string
	cfg Config
	now func() time.Time
	// Errorf, when set, receives write errors of Record.
	Errorf func(format string, args ...any)

	mu     sync.Mutex
	f      *os.File
	day    string
	pruned string // day of the last prune
}

// New returns a log writing to dir (created on the first entry).
func New(dir string, cfg Config) *Log { return &Log{dir: dir, cfg: cfg, now: time.Now} }

// Dir is the folder of the log files.
func (l *Log) Dir() string { return l.dir }

// Enabled reports whether Add records anything.
func (l *Log) Enabled() bool { return l != nil && l.cfg.AuditEnabled() }

// Add appends one entry. Time is set when zero. Errors are returned for the
// caller to log; nothing in the daemon depends on the write succeeding.
func (l *Log) Add(e Entry) error {
	if !l.Enabled() {
		return nil
	}
	if e.Time.IsZero() {
		e.Time = l.now()
	}
	e.Time = e.Time.UTC().Truncate(time.Millisecond)
	e.Target = clip(e.Target, maxTarget)
	e.Detail = clip(e.Detail, maxDetail)
	e.User = clip(e.User, 128)
	e.Env = clip(e.Env, 128)
	e.Origin = clip(e.Origin, 256)
	if e.Result == "" {
		e.Result = OK
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	// Once a day, after the write and outside the lock: delete the files past the retention.
	prune := false
	defer func() {
		if prune {
			l.Prune()
		}
	}()
	l.mu.Lock()
	defer l.mu.Unlock()
	day := e.Time.Format(dayLayout)
	if today := l.now().UTC().Format(dayLayout); day > today {
		day = today // a clock that went back: never write into a future file
	}
	if l.f == nil || l.day != day {
		if l.f != nil {
			l.f.Close()
			l.f = nil
		}
		if err := os.MkdirAll(l.dir, 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(filepath.Join(l.dir, filePrefix+day+fileSuffix), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			return err
		}
		l.f, l.day = f, day
	}
	if l.pruned != day {
		l.pruned = day
		prune = true
	}
	_, err = l.f.Write(line)
	if runtime.GOOS == "windows" {
		// Windows cannot delete or rename a file that has an open handle (Go
		// does not share FILE_SHARE_DELETE), which would block retention,
		// rotation and any admin cleanup: close after every write.
		if cerr := l.f.Close(); err == nil {
			err = cerr
		}
		l.f = nil
	}
	return err
}

// Close closes the current file.
func (l *Log) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
}

// Prune deletes the files older than the retention period.
func (l *Log) Prune() {
	days := l.cfg.AuditRetentionDays()
	if days <= 0 {
		return
	}
	cut := l.now().UTC().AddDate(0, 0, -days).Format(dayLayout)
	files, _ := l.files()
	for _, f := range files {
		if f.day < cut {
			_ = os.Remove(filepath.Join(l.dir, f.name))
		}
	}
}

type logFile struct{ name, day string }

// files lists the log files, newest first.
func (l *Log) files() ([]logFile, error) {
	ents, err := os.ReadDir(l.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []logFile
	for _, e := range ents {
		n := e.Name()
		if !strings.HasPrefix(n, filePrefix) || !strings.HasSuffix(n, fileSuffix) || e.IsDir() {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(n, filePrefix), fileSuffix)
		if _, err := time.Parse(dayLayout, day); err != nil {
			continue
		}
		out = append(out, logFile{n, day})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].day > out[j].day })
	return out, nil
}

// Query selects entries. Zero fields match everything.
type Query struct {
	Plugin string
	User   string
	Source string
	Action string
	// Text matches a substring of the target, in any case.
	Text  string
	Since time.Time
	Until time.Time
	Limit int
	// Cursor is the Next of the previous page.
	Cursor string
}

func (q *Query) match(e *Entry) bool {
	if q.Plugin != "" && e.Plugin != q.Plugin {
		return false
	}
	if q.User != "" && e.User != q.User {
		return false
	}
	if q.Source != "" && e.Source != q.Source {
		return false
	}
	if q.Action != "" && e.Action != q.Action {
		return false
	}
	if !q.Since.IsZero() && e.Time.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && e.Time.After(q.Until) {
		return false
	}
	if q.Text != "" && !strings.Contains(strings.ToLower(e.Target+" "+e.Detail), strings.ToLower(q.Text)) {
		return false
	}
	return true
}

// ErrCursor is returned for a cursor that List did not produce.
var ErrCursor = errors.New("invalid cursor")

func parseCursor(c string) (day string, line int, err error) {
	if c == "" {
		return "", 0, nil
	}
	d, n, ok := strings.Cut(c, ":")
	if _, perr := time.Parse(dayLayout, d); !ok || perr != nil {
		return "", 0, ErrCursor
	}
	line, perr := strconv.Atoi(n)
	if perr != nil || line < 0 {
		return "", 0, ErrCursor
	}
	return d, line, nil
}

type pos struct {
	e    Entry
	day  string
	line int
}

// List returns the matching entries, newest first, and the cursor of the
// next page ("" when there is none).
func (l *Log) List(q Query) ([]Entry, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	curDay, curLine, err := parseCursor(q.Cursor)
	if err != nil {
		return nil, "", err
	}
	files, err := l.files()
	if err != nil {
		return nil, "", err
	}
	var out []pos
	more := false
	for _, f := range files {
		if curDay != "" && f.day > curDay {
			continue
		}
		if !q.Since.IsZero() && f.day < q.Since.UTC().Format(dayLayout) {
			break
		}
		if !q.Until.IsZero() && f.day > q.Until.UTC().Format(dayLayout) {
			continue
		}
		before := -1
		if f.day == curDay {
			before = curLine
		}
		need := limit - len(out)
		got, over, err := l.scan(f, &q, before, need)
		if err != nil {
			return nil, "", err
		}
		out = append(out, got...)
		if over {
			more = true
			break
		}
	}
	entries := make([]Entry, len(out))
	for i := range out {
		entries[i] = out[i].e
	}
	next := ""
	if len(out) == limit && (more || len(files) > 1) {
		last := out[len(out)-1]
		next = fmt.Sprintf("%s:%d", last.day, last.line)
	}
	return entries, next, nil
}

// scan returns up to need matching entries of f with a line number below
// before (when >= 0), newest first, and whether older matches remain.
func (l *Log) scan(f logFile, q *Query, before, need int) ([]pos, bool, error) {
	fh, err := os.Open(filepath.Join(l.dir, f.name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	ring := make([]pos, 0, need+1)
	n := -1
	for sc.Scan() {
		n++
		if before >= 0 && n >= before {
			break
		}
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) != nil || !q.match(&e) {
			continue
		}
		if len(ring) == need+1 {
			copy(ring, ring[1:])
			ring = ring[:need]
		}
		ring = append(ring, pos{e, f.day, n})
	}
	over := len(ring) > need
	if over {
		ring = ring[1:]
	}
	// newest first
	for i, j := 0, len(ring)-1; i < j; i, j = i+1, j-1 {
		ring[i], ring[j] = ring[j], ring[i]
	}
	return ring, over, nil
}

// Each calls fn for every matching entry, oldest first (used by exports).
// It stops at fn's first error.
func (l *Log) Each(q Query, fn func(Entry) error) error {
	files, err := l.files()
	if err != nil {
		return err
	}
	for i := len(files) - 1; i >= 0; i-- {
		f := files[i]
		if !q.Since.IsZero() && f.day < q.Since.UTC().Format(dayLayout) {
			continue
		}
		if !q.Until.IsZero() && f.day > q.Until.UTC().Format(dayLayout) {
			continue
		}
		fh, err := os.Open(filepath.Join(l.dir, f.name))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 64<<10), maxLine)
		for sc.Scan() {
			var e Entry
			if json.Unmarshal(sc.Bytes(), &e) != nil || !q.match(&e) {
				continue
			}
			if err := fn(e); err != nil {
				fh.Close()
				return err
			}
		}
		fh.Close()
	}
	return nil
}

func clip(s string, max int) string {
	if len(s) <= max {
		return strings.ToValidUTF8(s, "?")
	}
	s = strings.ToValidUTF8(s[:max], "?")
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

// ---- redaction ------------------------------------------------------------

// secretName matches names of parameters, flags and environment variables
// that carry secrets.
var secretName = regexp.MustCompile(`(?i)(pass(word|wd|phrase)?|secret|token|auth|credential|api[-_]?key|private[-_]?key|access[-_]?key|bearer|cookie|session|registry[-_]?config|build[-_]?args)`)

// userInfo matches user:password@ in a URL. The password may hold a "/"
// (some tools take "https://u:pa/ss@host" as user u, password pa/ss), so it
// runs to the "@"; a URL with a port and an "@" later in its path is
// over-redacted, which is the safe side.
var userInfo = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://[^/\s:@]*):[^\s@]*@`)

// bearer matches "Bearer x" and authHeader "Authorization: <scheme> x" (or
// "Authorization: x") written into an argument (curl -H ...).
var (
	bearer     = regexp.MustCompile(`(?i)\b(bearer\s+)[^\s'"]+`)
	authHeader = regexp.MustCompile(`(?i)\b((?:proxy-)?authorization\s*:\s*(?:[a-z0-9-]+\s+)?)[^\s'"]+`)
)

// portMapping is what follows a short -p that is not a password: a port
// mapping like 8080:80 or 127.0.0.1:8080:80/tcp (docker run -p).
var portMapping = regexp.MustCompile(`^([0-9.]+:)?[0-9]{1,5}(-[0-9]{1,5})?:[0-9]{1,5}(-[0-9]{1,5})?(/(tcp|udp|sctp))?$`)

// Redacted replaces a secret.
const Redacted = "***"

// RedactText removes passwords from URLs inside s.
func RedactText(s string) string { return redactValue(s) }

// RedactURL removes the password and the secret query values of a URL.
func RedactURL(u string) string {
	base, q, hasQ := strings.Cut(u, "?")
	base = redactValue(base)
	if hasQ {
		return base + "?" + RedactQuery(q)
	}
	return base
}

// SecretName reports whether a parameter or flag name looks like a secret.
func SecretName(name string) bool { return secretName.MatchString(name) }

// redactValue removes a password from URLs inside s and the credentials
// of an authorization scheme ("Bearer x").
func redactValue(s string) string {
	s = userInfo.ReplaceAllString(s, "$1:"+Redacted+"@")
	s = bearer.ReplaceAllString(s, "${1}"+Redacted)
	return authHeader.ReplaceAllString(s, "${1}"+Redacted)
}

// RedactQuery redacts the values of a raw query string whose keys look like
// secrets, keeping the rest ("a=1&token=***").
func RedactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	for i, p := range parts {
		k, v, hasV := strings.Cut(p, "=")
		name := k
		if u, err := url.QueryUnescape(k); err == nil {
			name = u // "access%5Ftoken" is access_token
		}
		if SecretName(name) && hasV && v != "" {
			parts[i] = k + "=" + Redacted
		} else if hasV {
			parts[i] = k + "=" + redactValue(v)
		}
	}
	return strings.Join(parts, "&")
}

// HTTPTarget is "METHOD /path?query" with secrets removed. Request headers
// and bodies are never part of it.
func HTTPTarget(method, path, query string) string {
	t := method + " " + path
	if q := RedactQuery(query); q != "" {
		t += "?" + q
	}
	return clip(t, maxTarget)
}

// CommandTarget is "command arg arg…" with secrets removed: the value of
// --password=x or KEY=x pairs whose name looks like a secret (also inside a
// flag value: --env=DB_PASSWORD=x, --build-arg=NPM_TOKEN=x), the word after
// --password / --token style flags and after a short -p (docker login -p,
// mysql -p; a port mapping such as 8080:80 is kept), passwords inside URLs
// and the credentials of "Bearer x" / "Basic x".
func CommandTarget(command string, args []string) string {
	var b strings.Builder
	b.WriteString(command)
	hide := false
	for _, a := range args {
		b.WriteByte(' ')
		switch {
		case hide:
			hide = false
			if portMapping.MatchString(a) {
				b.WriteString(a)
			} else {
				b.WriteString(Redacted)
			}
			continue
		case a == "-p":
			hide = true
			b.WriteString(a)
			continue
		case strings.HasPrefix(a, "-"):
			name, val, hasV := strings.Cut(strings.TrimLeft(a, "-"), "=")
			if SecretName(name) {
				if hasV {
					b.WriteString(a[:len(a)-len(val)] + Redacted)
				} else {
					hide = true
					b.WriteString(a)
				}
				continue
			}
			if hasV {
				if k, v, ok := strings.Cut(val, "="); ok && v != "" && SecretName(k) {
					b.WriteString(clip(a[:len(a)-len(val)]+k+"="+Redacted, maxArg))
					continue
				}
			}
		default:
			if k, v, ok := strings.Cut(a, "="); ok && v != "" && SecretName(k) && !strings.ContainsAny(k, " /") {
				b.WriteString(clip(k, maxArg) + "=" + Redacted)
				continue
			}
		}
		b.WriteString(clip(redactValue(a), maxArg))
	}
	return clip(b.String(), maxTarget)
}

// CSVRecord is the entry as the columns of the CSV export.
func CSVRecord(e Entry) []string {
	code, size := "", ""
	if e.Code != nil {
		code = strconv.Itoa(*e.Code)
	}
	if e.Bytes != nil {
		size = strconv.FormatInt(*e.Bytes, 10)
	}
	// Every text column goes through csvSafe: user names, plugin ids and
	// the rest can come from a request. time, code, bytes and admin are
	// written here (a negative exit code stays a number).
	return []string{
		e.Time.UTC().Format(time.RFC3339), csvSafe(e.User), csvSafe(e.IP), csvSafe(e.Source), csvSafe(e.Plugin), csvSafe(e.Action), csvSafe(e.Via),
		csvSafe(e.Target), csvSafe(e.Result), code, size, strconv.FormatBool(e.Admin), csvSafe(e.Detail), csvSafe(e.Env), csvSafe(e.Origin),
	}
}

// CSVHeader are the columns of CSVRecord.
var CSVHeader = []string{"time", "user", "ip", "source", "plugin", "action", "via", "target", "result", "code", "bytes", "admin", "detail", "env", "origin"}

// csvSafe stops a spreadsheet from running a value as a formula: a value
// that starts (after any spaces) with = + - @, a tab, CR or LF gets a
// leading quote.
func csvSafe(s string) string {
	t := strings.TrimLeft(s, " ")
	if t != "" && strings.ContainsRune("=+-@\t\r\n", rune(t[0])) {
		return "'" + s
	}
	return s
}
