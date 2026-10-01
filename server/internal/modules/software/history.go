package software

import (
	"bufio"
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// HistoryEntry is one package change from the package manager's log.
type HistoryEntry struct {
	Time   int64  `json:"time"`   // unix ms
	Action string `json:"action"` // installed | upgraded | removed | downgraded | reinstalled
	Name   string `json:"name"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Tx     int    `json:"tx"` // entries of one transaction share the number
}

const maxLogRead = 12 << 20

// readTail reads up to maxLogRead bytes from the end of a file.
func readTail(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if fi.Size() > maxLogRead {
		if _, err := f.Seek(fi.Size()-maxLogRead, io.SeekStart); err != nil {
			return "", err
		}
	}
	b, err := io.ReadAll(f)
	return string(b), err
}

var pacmanLogRe = regexp.MustCompile(`^\[([^\]]+)\] \[ALPM\] (installed|upgraded|removed|downgraded|reinstalled) (\S+) \((.+)\)$`)

// parsePacmanLog parses /var/log/pacman.log, oldest first.
func parsePacmanLog(content string) []HistoryEntry {
	var out []HistoryEntry
	tx := 0
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasSuffix(line, "] transaction started") {
			tx++
			continue
		}
		g := pacmanLogRe.FindStringSubmatch(line)
		if g == nil {
			continue
		}
		t, err := time.Parse("2006-01-02T15:04:05-0700", g[1])
		if err != nil {
			continue
		}
		e := HistoryEntry{Time: t.UnixMilli(), Action: g[2], Name: g[3], Tx: tx}
		if from, to, ok := strings.Cut(g[4], " -> "); ok {
			e.From, e.To = from, to
		} else if g[2] == "removed" {
			e.From = g[4]
		} else {
			e.To = g[4]
		}
		out = append(out, e)
	}
	return out
}

var aptPkgRe = regexp.MustCompile(`(\S+?)(?::\w+)? \(([^)]*)\)`)

// parseAptHistory parses /var/log/apt/history.log.
func parseAptHistory(content string) []HistoryEntry {
	var out []HistoryEntry
	tx := 0
	var when int64
	for _, line := range strings.Split(content, "\n") {
		if v, ok := strings.CutPrefix(line, "Start-Date:"); ok {
			tx++
			t, err := time.ParseInLocation("2006-01-02  15:04:05", strings.TrimSpace(v), time.Local)
			if err != nil {
				t, _ = time.ParseInLocation("2006-01-02 15:04:05", strings.Join(strings.Fields(v), " "), time.Local)
			}
			when = t.UnixMilli()
			continue
		}
		key, rest, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		var action string
		switch key {
		case "Install":
			action = "installed"
		case "Upgrade":
			action = "upgraded"
		case "Remove", "Purge":
			action = "removed"
		case "Downgrade":
			action = "downgraded"
		case "Reinstall":
			action = "reinstalled"
		default:
			continue
		}
		for _, g := range aptPkgRe.FindAllStringSubmatch(rest, -1) {
			vers := strings.Split(g[2], ", ")
			e := HistoryEntry{Time: when, Action: action, Name: g[1], Tx: tx}
			switch {
			case action == "removed":
				e.From = vers[0]
			case len(vers) >= 2 && (action == "upgraded" || action == "downgraded"):
				e.From, e.To = vers[0], vers[1]
			default:
				e.To = vers[0]
			}
			out = append(out, e)
		}
	}
	return out
}

// parseZypperHistory parses /var/log/zypp/history.
func parseZypperHistory(content string) []HistoryEntry {
	var out []HistoryEntry
	tx := 0
	var lastTime string
	for _, line := range strings.Split(content, "\n") {
		f := strings.Split(line, "|")
		if len(f) < 4 || strings.HasPrefix(line, "#") {
			continue
		}
		var action string
		switch f[1] {
		case "install":
			action = "installed"
		case "remove":
			action = "removed"
		default:
			continue
		}
		t, err := time.ParseInLocation("2006-01-02 15:04:05", f[0], time.Local)
		if err != nil {
			continue
		}
		if f[0] != lastTime {
			tx++
			lastTime = f[0]
		}
		e := HistoryEntry{Time: t.UnixMilli(), Action: action, Name: f[2], Tx: tx}
		if action == "removed" {
			e.From = f[3]
		} else {
			e.To = f[3]
		}
		out = append(out, e)
	}
	return out
}

var dnfLogRe = regexp.MustCompile(`^(\S+) \w+ (Installed|Upgrade|Erase|Downgrade|Reinstall): (.+)$`)
var rpmNEVRARe = regexp.MustCompile(`^(.+)-([^-]+-[^-]+)\.\w+$`)

// parseDnfLog parses /var/log/dnf.rpm.log.
func parseDnfLog(content string) []HistoryEntry {
	var out []HistoryEntry
	tx := 0
	var last int64
	for _, line := range strings.Split(content, "\n") {
		g := dnfLogRe.FindStringSubmatch(line)
		if g == nil {
			continue
		}
		t, err := time.Parse("2006-01-02T15:04:05Z0700", g[1])
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05Z", g[1])
			if err != nil {
				continue
			}
		}
		if t.Unix()-last > 120 {
			tx++
		}
		last = t.Unix()
		nv := rpmNEVRARe.FindStringSubmatch(strings.TrimSpace(g[3]))
		if nv == nil {
			continue
		}
		e := HistoryEntry{Time: t.UnixMilli(), Name: nv[1], Tx: tx}
		switch g[2] {
		case "Installed":
			e.Action, e.To = "installed", nv[2]
		case "Upgrade":
			e.Action, e.To = "upgraded", nv[2]
		case "Erase":
			e.Action, e.From = "removed", nv[2]
		case "Downgrade":
			e.Action, e.To = "downgraded", nv[2]
		default:
			e.Action, e.To = "reinstalled", nv[2]
		}
		out = append(out, e)
	}
	return out
}

// history returns the newest entries first.
func (m *manager) history(ctx context.Context, limit int) ([]HistoryEntry, error) {
	var entries []HistoryEntry
	read := func(path string, parse func(string) []HistoryEntry) {
		if s, err := readTail(path); err == nil {
			entries = parse(s)
		}
	}
	if m.primary != nil {
		switch m.primary.Name() {
		case "pacman":
			read("/var/log/pacman.log", parsePacmanLog)
		case "apt":
			read("/var/log/apt/history.log", parseAptHistory)
		case "zypper":
			read("/var/log/zypp/history", parseZypperHistory)
		case "dnf":
			read("/var/log/dnf.rpm.log", parseDnfLog)
		}
	}
	// newest first
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	if entries == nil {
		entries = []HistoryEntry{}
	}
	return entries, nil
}
