package logs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
	"github.com/Fonlogen/LinuxAdmin/server/internal/sys"
)

// jstr decodes a journald JSON field: a string, an array of byte values
// (binary data) or an array of strings (repeated field, first one wins).
type jstr string

func (j *jstr) UnmarshalJSON(b []byte) error {
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*j = jstr(s)
		return nil
	}
	var nums []int
	if err := json.Unmarshal(b, &nums); err == nil {
		buf := make([]byte, len(nums))
		for i, n := range nums {
			buf[i] = byte(n)
		}
		*j = jstr(strings.ToValidUTF8(string(buf), "�"))
		return nil
	}
	var strs []string
	if err := json.Unmarshal(b, &strs); err == nil && len(strs) > 0 {
		*j = jstr(strs[0])
	}
	return nil
}

type jline struct {
	Realtime  jstr `json:"__REALTIME_TIMESTAMP"`
	Cursor    jstr `json:"__CURSOR"`
	Priority  jstr `json:"PRIORITY"`
	Message   jstr `json:"MESSAGE"`
	Unit      jstr `json:"_SYSTEMD_UNIT"`
	UserUnit  jstr `json:"_SYSTEMD_USER_UNIT"`
	Ident     jstr `json:"SYSLOG_IDENTIFIER"`
	Comm      jstr `json:"_COMM"`
	Pid       jstr `json:"_PID"`
	Transport jstr `json:"_TRANSPORT"`
}

// parseJournalLine converts one line of `journalctl -o json` into an Entry.
func parseJournalLine(line string) (*Entry, bool) {
	var j jline
	if err := json.Unmarshal([]byte(line), &j); err != nil {
		return nil, false
	}
	us, err := strconv.ParseInt(string(j.Realtime), 10, 64)
	if err != nil {
		return nil, false
	}
	e := &Entry{TsUs: us, Ts: us / 1000, Level: LvInfo, Cursor: string(j.Cursor), SrcID: "journal"}
	if p, err := strconv.Atoi(string(j.Priority)); err == nil {
		e.Level = levelFromPriority(p)
	}
	e.Message = clip(strings.TrimRight(string(j.Message), "\n"))
	unit := string(j.Unit)
	if unit == "" {
		unit = string(j.UserUnit)
	}
	e.Unit = unit
	switch {
	case j.Transport == "kernel":
		e.Source = "kernel"
	case j.Ident != "":
		e.Source = string(j.Ident)
	case unit != "":
		e.Source = strings.TrimSuffix(unit, ".service")
	case j.Comm != "":
		e.Source = string(j.Comm)
	default:
		e.Source = "journal"
	}
	if n, err := strconv.Atoi(string(j.Pid)); err == nil {
		e.Pid = n
	}
	return e, true
}

// ---------- access ----------

// journalReadable reports whether this process can read the system journal
// files. When nothing is found (containers, no persistent journal) it
// returns true and lets journalctl decide.
func journalReadable() bool {
	denied := false
	for _, d := range []string{"/var/log/journal", "/run/log/journal"} {
		ents, err := os.ReadDir(d)
		if err != nil {
			if os.IsPermission(err) {
				denied = true
			}
			continue
		}
		for _, m := range ents {
			if !m.IsDir() {
				continue
			}
			files, err := os.ReadDir(filepath.Join(d, m.Name()))
			if err != nil {
				if os.IsPermission(err) {
					denied = true
				}
				continue
			}
			for _, f := range files {
				if !strings.HasSuffix(f.Name(), ".journal") {
					continue
				}
				fh, err := os.Open(filepath.Join(d, m.Name(), f.Name()))
				if err == nil {
					fh.Close()
					return true
				}
				if os.IsPermission(err) {
					denied = true
				}
			}
		}
	}
	return !denied
}

func errNeedsAdmin() error {
	return rpc.Errorf(rpc.NeedsAdmin, "Reading the system journal needs administrator rights, or membership of the systemd-journal group.")
}

// ---------- journalctl invocation ----------

// jgroup is one journalctl invocation: a set of match arguments.
type jgroup struct {
	name string
	args []string
}

// journalGroups turns source specs into the journalctl invocations needed.
func journalGroups(specs []spec) []jgroup {
	var units []string
	kernel, boot := false, false
	for _, s := range specs {
		switch s.Kind {
		case "journal":
			return []jgroup{{name: "journal"}}
		case "boot":
			boot = true
		case "kernel":
			kernel = true
		case "unit":
			units = append(units, "--unit="+s.Unit)
		}
	}
	var out []jgroup
	if boot {
		return []jgroup{{name: "boot", args: []string{"--boot"}}}
	}
	if len(units) > 0 {
		out = append(out, jgroup{name: "units", args: units})
	}
	if kernel {
		out = append(out, jgroup{name: "kernel", args: []string{"--dmesg"}})
	}
	return out
}

func hasJournal(specs []spec) bool {
	for _, s := range specs {
		if s.Kind != "file" {
			return true
		}
	}
	return false
}

func usArg(us int64) string {
	return fmt.Sprintf("@%d.%06d", us/1000000, us%1000000)
}

// priorityRange returns the journald -p range covering the allowed levels.
func priorityRange(levels map[string]bool) (lo, hi int, all bool) {
	if levels == nil {
		return 0, 7, true
	}
	lo, hi = 8, -1
	add := func(a, b int) {
		if a < lo {
			lo = a
		}
		if b > hi {
			hi = b
		}
	}
	if levels[LvErr] {
		add(0, 3)
	}
	if levels[LvWarn] {
		add(4, 4)
	}
	if levels[LvInfo] {
		add(5, 6)
	}
	if levels[LvDebug] {
		add(7, 7)
	}
	if hi < 0 {
		return 0, 7, true
	}
	return lo, hi, lo == 0 && hi == 7
}

// journalArgs builds the arguments for a journalctl run. T is the newest
// timestamp wanted (noLimit = none).
func journalArgs(g jgroup, f filter, until int64, extra ...string) []string {
	args := []string{"-o", "json", "--no-pager", "-q"}
	args = append(args, g.args...)
	if f.sinceUs > 0 {
		args = append(args, "--since="+usArg(f.sinceUs))
	}
	if until != noLimit {
		args = append(args, "--until="+usArg(until))
	}
	if lo, hi, all := priorityRange(f.levels); !all {
		if lo == hi {
			args = append(args, "-p", strconv.Itoa(lo))
		} else {
			args = append(args, "-p", fmt.Sprintf("%d..%d", lo, hi))
		}
	}
	if f.text != "" {
		args = append(args, "--case-sensitive=false", "--grep="+regexp.QuoteMeta(f.text))
	}
	return append(args, extra...)
}

var errStop = errors.New("stop")

func journalErr(err error) error {
	if err == nil || errors.Is(err, errStop) {
		return nil
	}
	var ee *sys.ExitError
	if errors.As(err, &ee) {
		if ee.Code == 1 && strings.TrimSpace(ee.Stderr) == "" {
			return nil // journalctl exits 1 when nothing matches
		}
		low := strings.ToLower(ee.Stderr)
		if strings.Contains(low, "permission") || strings.Contains(low, "insufficient") {
			return errNeedsAdmin()
		}
	}
	return err
}

// fetchJournal returns up to want entries with TsUs <= until, newest first.
func fetchJournal(ctx context.Context, g jgroup, f filter, until int64, want int) ([]Entry, error) {
	args := journalArgs(g, f, until, "-r", "-n", strconv.Itoa(want*2+50))
	var out []Entry
	err := sys.Stream(ctx, "journalctl", args, func(line string) error {
		e, ok := parseJournalLine(line)
		if !ok || e.TsUs > until {
			return nil
		}
		if !f.match(e) {
			return nil
		}
		out = append(out, *e)
		if len(out) >= want {
			return errStop
		}
		return nil
	})
	return out, journalErr(err)
}
