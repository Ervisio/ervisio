package logs

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func cleanPath(p string) string { return filepath.Clean(p) }

const maxMessage = 8000

func clip(s string) string {
	if len(s) <= maxMessage {
		return s
	}
	cut := maxMessage
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

// ---------- levels ----------

const kwEnd = `(?:[^\w.]|\.(?:\s|$)|$)`
const kwStart = `(?:^|[^\w./-])`

var (
	reErr   = regexp.MustCompile(`(?i)` + kwStart + `(?:fatal|crit(?:ical)?|emerg(?:ency)?|alert|panic|error|err|severe)` + kwEnd)
	reWarn  = regexp.MustCompile(`(?i)` + kwStart + `(?:warn(?:ing)?)` + kwEnd)
	reDebug = regexp.MustCompile(`(?i)` + kwStart + `(?:debug|dbg)` + kwEnd)
)

// detectLevel guesses the level of a plain-text line by keywords. It returns
// "" when nothing matches.
func detectLevel(msg string) string {
	if len(msg) > 300 {
		msg = msg[:300]
	}
	switch {
	case reErr.MatchString(msg):
		return LvErr
	case reWarn.MatchString(msg):
		return LvWarn
	case reDebug.MatchString(msg):
		return LvDebug
	}
	return ""
}

// levelFromString maps level names used by common loggers.
func levelFromString(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "err", "error", "fatal", "critical", "crit", "emerg", "emergency", "alert", "panic", "severe", "e", "f", "c":
		return LvErr
	case "warn", "warning", "w":
		return LvWarn
	case "debug", "trace", "verbose", "d", "t", "v", "dbg":
		return LvDebug
	case "info", "notice", "information", "i", "n", "informational":
		return LvInfo
	}
	if n, err := strconv.Atoi(s); err == nil {
		return levelFromNumber(float64(n))
	}
	return ""
}

func levelFromNumber(n float64) string {
	if n >= 10 { // pino / bunyan: 10 trace .. 60 fatal
		switch {
		case n >= 50:
			return LvErr
		case n >= 40:
			return LvWarn
		case n >= 30:
			return LvInfo
		}
		return LvDebug
	}
	return levelFromPriority(int(n))
}

// levelFromPriority maps syslog priorities 0-7.
func levelFromPriority(p int) string {
	switch {
	case p <= 3:
		return LvErr
	case p == 4:
		return LvWarn
	case p <= 6:
		return LvInfo
	}
	return LvDebug
}

// ---------- timestamps ----------

var (
	reISO    = regexp.MustCompile(`^\s*\[?(\d{4})[-/](\d{2})[-/](\d{2})[T ](\d{2}):(\d{2}):(\d{2})(?:[.,](\d{1,9}))?\s*(Z|[+-]\d{2}:?\d{2})?\]?[:\s]*`)
	reSyslog = regexp.MustCompile(`^\s*([A-Z][a-z]{2})\s+(\d{1,2})\s+(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?\s*`)
	reApache = regexp.MustCompile(`\[(\d{2})/([A-Za-z]{3})/(\d{4}):(\d{2}):(\d{2}):(\d{2})(?: ([+-]\d{4}))?\]`)
)

var months = map[string]time.Month{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func zoneOf(z string, loc *time.Location) *time.Location {
	if z == "" {
		return loc
	}
	if z == "Z" {
		return time.UTC
	}
	z = strings.Replace(z, ":", "", 1)
	if len(z) != 5 {
		return loc
	}
	off := (atoi(z[1:3])*60 + atoi(z[3:5])) * 60
	if z[0] == '-' {
		off = -off
	}
	return time.FixedZone("", off)
}

// parseTime finds a timestamp at the start of a line (ISO 8601, nginx error
// "2006/01/02 15:04:05", syslog "Jan _2 15:04:05") or an Apache/nginx access
// log "[02/Jan/2006:15:04:05 +0000]". rest is the line without the leading
// timestamp (the whole line for the access-log form).
func parseTime(line string, now time.Time, loc *time.Location) (t time.Time, rest string, ok bool) {
	head := line
	if len(head) > 200 {
		head = head[:200]
	}
	if m := reISO.FindStringSubmatchIndex(head); m != nil {
		g := func(i int) string { return head[m[2*i]:m[2*i+1]] }
		var ns int
		if m[2*7] >= 0 {
			f := g(7)
			for len(f) < 9 {
				f += "0"
			}
			ns = atoi(f)
		}
		var z string
		if m[2*8] >= 0 {
			z = g(8)
		}
		t = time.Date(atoi(g(1)), time.Month(atoi(g(2))), atoi(g(3)), atoi(g(4)), atoi(g(5)), atoi(g(6)), ns, zoneOf(z, loc))
		return t, line[m[1]:], true
	}
	if m := reSyslog.FindStringSubmatchIndex(head); m != nil {
		g := func(i int) string { return head[m[2*i]:m[2*i+1]] }
		mon, found := months[strings.ToLower(g(1))]
		if found {
			t = time.Date(now.Year(), mon, atoi(g(2)), atoi(g(3)), atoi(g(4)), atoi(g(5)), 0, loc)
			if t.After(now.Add(24 * time.Hour)) {
				t = t.AddDate(-1, 0, 0)
			}
			return t, line[m[1]:], true
		}
	}
	if strings.IndexByte(head, '[') >= 0 {
		if m := reApache.FindStringSubmatch(head); m != nil {
			mon, found := months[strings.ToLower(m[2])]
			if found {
				t = time.Date(atoi(m[3]), mon, atoi(m[1]), atoi(m[4]), atoi(m[5]), atoi(m[6]), 0, zoneOf(m[7], loc))
				return t, line, true
			}
		}
	}
	return time.Time{}, line, false
}

// ---------- JSON lines ----------

var (
	jsonTimeKeys  = []string{"time", "ts", "timestamp", "@timestamp", "t", "datetime", "asctime"}
	jsonLevelKeys = []string{"level", "severity", "lvl", "levelname", "log.level", "loglevel"}
	jsonMsgKeys   = []string{"msg", "message", "log", "event", "text"}
)

func numberToTime(f float64) (time.Time, bool) {
	switch {
	case f > 1e17:
		return time.Unix(0, int64(f)), true
	case f > 1e14:
		return time.UnixMicro(int64(f)), true
	case f > 1e11:
		return time.UnixMilli(int64(f)), true
	case f > 1e8:
		return time.Unix(int64(f), int64((f-float64(int64(f)))*1e9)), true
	}
	return time.Time{}, false
}

// parseJSONLine reads a structured log line. ok is false when line is not a
// JSON object. The returned time is zero and level "" when the fields are
// missing.
func parseJSONLine(line string, now time.Time, loc *time.Location) (msg, level string, t time.Time, ok bool) {
	s := strings.TrimSpace(line)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return
	}
	ok = true
	for _, k := range jsonTimeKeys {
		v, has := m[k]
		if !has {
			continue
		}
		switch x := v.(type) {
		case float64:
			if tt, good := numberToTime(x); good {
				t = tt
			}
		case string:
			if tt, _, good := parseTime(x, now, loc); good {
				t = tt
			} else if tt, err := time.Parse(time.RFC3339Nano, x); err == nil {
				t = tt
			}
		}
		if !t.IsZero() {
			break
		}
	}
	for _, k := range jsonLevelKeys {
		v, has := m[k]
		if !has {
			continue
		}
		switch x := v.(type) {
		case string:
			level = levelFromString(x)
		case float64:
			level = levelFromNumber(x)
		}
		if level != "" {
			break
		}
	}
	for _, k := range jsonMsgKeys {
		if v, has := m[k]; has {
			if str, isStr := v.(string); isStr && str != "" {
				msg = strings.TrimRight(str, "\n")
				break
			}
		}
	}
	if msg == "" {
		msg = s
	}
	return
}

// lineEntry builds an entry from one text line. hasTs reports whether a
// timestamp was found; Level is "" when it could not be detected.
func lineEntry(text, label, path string, no int, format string, now time.Time, loc *time.Location) (e *Entry, hasTs bool) {
	e = &Entry{Source: label, SrcID: "file:" + path, File: path, Line: no}
	e.Cursor = "file:" + path + ":" + strconv.Itoa(no)
	var t time.Time
	if format != "plain" {
		if msg, lvl, tt, ok := parseJSONLine(text, now, loc); ok {
			e.Message = clip(msg)
			e.Level = lvl
			if lvl == "" {
				e.Level = detectLevel(msg)
			}
			if !tt.IsZero() {
				e.setTime(tt)
				hasTs = true
			}
			if e.Message != text {
				e.Raw = clip(text)
			}
			return
		}
	}
	msg := text
	t, rest, ok := parseTime(text, now, loc)
	if ok {
		hasTs = true
		e.setTime(t)
		msg = rest
	}
	e.Message = clip(msg)
	if msg != text {
		e.Raw = clip(text)
	}
	if format == "plain" {
		e.Level = LvInfo
	} else {
		e.Level = detectLevel(msg)
	}
	return
}

func (e *Entry) setTime(t time.Time) {
	e.TsUs = t.UnixMicro()
	e.Ts = e.TsUs / 1000
}
