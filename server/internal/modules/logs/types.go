package logs

import (
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// Entry is one log line, from the journal or from a file.
type Entry struct {
	Ts      int64  `json:"ts"`   // milliseconds since the epoch
	TsUs    int64  `json:"tsUs"` // microseconds since the epoch
	Level   string `json:"level"`
	Source  string `json:"source"`
	SrcID   string `json:"srcId"`
	Message string `json:"message"`
	Raw     string `json:"raw,omitempty"` // the full original line when Message has the timestamp removed
	Unit    string `json:"unit,omitempty"`
	Pid     int    `json:"pid,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Cursor  string `json:"cursor"`
}

// Levels used by the API.
const (
	LvErr   = "err"
	LvWarn  = "warn"
	LvInfo  = "info"
	LvDebug = "debug"
)

var allLevels = []string{LvErr, LvWarn, LvInfo, LvDebug}

const noLimit = int64(math.MaxInt64)

// filter holds the constraints shared by query, histogram and follow.
type filter struct {
	sinceUs int64           // 0 = no lower bound
	untilUs int64           // noLimit = no upper bound (inclusive)
	levels  map[string]bool // nil = every level
	text    string          // lower-cased substring, "" = none
}

func (f filter) levelOK(l string) bool { return f.levels == nil || f.levels[l] }

func (f filter) textOK(msg string) bool {
	return f.text == "" || strings.Contains(strings.ToLower(msg), f.text)
}

func (f filter) match(e *Entry) bool {
	return f.levelOK(e.Level) && f.textOK(e.Message)
}

func newFilter(sinceMs, untilMs int64, levels []string, text string) (filter, error) {
	f := filter{untilUs: noLimit}
	if sinceMs > 0 {
		f.sinceUs = sinceMs * 1000
	}
	if untilMs > 0 {
		f.untilUs = untilMs*1000 + 999
	}
	if len(levels) > 0 && len(levels) < len(allLevels) {
		f.levels = map[string]bool{}
		for _, l := range levels {
			switch l {
			case LvErr, LvWarn, LvInfo, LvDebug:
				f.levels[l] = true
			default:
				return f, rpc.Errorf(rpc.Invalid, "unknown level %q (use err, warn, info or debug)", l)
			}
		}
	}
	text = strings.TrimSpace(text)
	if len(text) > 300 {
		return f, rpc.Errorf(rpc.Invalid, "search text is too long")
	}
	f.text = strings.ToLower(text)
	return f, nil
}

// Watcher is a user-defined log file (pref logs.watchers).
type Watcher struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Format   string `json:"format"` // plain | auto | json
	Notify   bool   `json:"notify"`
	KeepDays int    `json:"keepDays,omitempty"`
}

// spec is a parsed source id.
type spec struct {
	ID     string
	Kind   string // journal | kernel | boot | unit | file
	Unit   string
	Path   string
	Label  string
	Format string
}

var unitRe = regexp.MustCompile(`^[A-Za-z0-9:_.@][A-Za-z0-9:_.@\\-]{0,199}$`)

func parseSourceID(id string, watchers []Watcher) (spec, error) {
	switch {
	case id == "journal" || id == "kernel" || id == "boot":
		return spec{ID: id, Kind: id, Label: id}, nil
	case strings.HasPrefix(id, "unit:"):
		u := strings.TrimPrefix(id, "unit:")
		if !unitRe.MatchString(u) {
			return spec{}, rpc.Errorf(rpc.Invalid, "invalid unit name %q", u)
		}
		return spec{ID: id, Kind: "unit", Unit: u, Label: strings.TrimSuffix(u, ".service")}, nil
	case strings.HasPrefix(id, "file:"):
		p := strings.TrimPrefix(id, "file:")
		if err := checkPath(p); err != nil {
			return spec{}, err
		}
		s := spec{ID: id, Kind: "file", Path: p, Label: labelForPath(p), Format: "auto"}
		for _, w := range watchers {
			if w.Path == p {
				if w.Name != "" {
					s.Label = w.Name
				}
				if w.Format != "" {
					s.Format = w.Format
				}
			}
		}
		return s, nil
	}
	return spec{}, rpc.Errorf(rpc.Invalid, "unknown log source %q", id)
}

func checkPath(p string) error {
	if p == "" || p[0] != '/' || strings.ContainsRune(p, 0) || len(p) > 4096 {
		return rpc.Errorf(rpc.Invalid, "the log file path must be absolute")
	}
	if cleanPath(p) != p {
		return rpc.Errorf(rpc.Invalid, "the log file path must be clean (no .. or //)")
	}
	return nil
}

func labelForPath(p string) string {
	if rel, ok := strings.CutPrefix(p, "/var/log/"); ok {
		return strings.TrimSuffix(rel, ".log")
	}
	i := strings.LastIndexByte(p, '/')
	return p[i+1:]
}

func nowUs() int64 { return time.Now().UnixMicro() }
