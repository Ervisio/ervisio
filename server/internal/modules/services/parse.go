package services

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// listedUnit is one row of `systemctl list-units --output=json`.
type listedUnit struct {
	Name        string `json:"unit"`
	Load        string `json:"load"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Description string `json:"description"`
}

// parseListUnits parses `systemctl list-units --output=json` output, falling
// back to the plain table format of older systemd versions.
func parseListUnits(out []byte) ([]listedUnit, error) {
	trim := strings.TrimSpace(string(out))
	if strings.HasPrefix(trim, "[") {
		var us []listedUnit
		if err := json.Unmarshal([]byte(trim), &us); err != nil {
			return nil, fmt.Errorf("unreadable systemctl output: %w", err)
		}
		return us, nil
	}
	var us []listedUnit
	for _, line := range strings.Split(trim, "\n") {
		line = strings.TrimLeft(line, " \t●*×")
		f := strings.Fields(line)
		if len(f) < 4 || !strings.Contains(f[0], ".") {
			continue
		}
		u := listedUnit{Name: f[0], Load: f[1], Active: f[2], Sub: f[3]}
		if len(f) > 4 {
			u.Description = strings.Join(f[4:], " ")
		}
		us = append(us, u)
	}
	return us, nil
}

// parseUnitFiles parses `systemctl list-unit-files` into name -> state.
func parseUnitFiles(out []byte) (map[string]string, error) {
	m := map[string]string{}
	trim := strings.TrimSpace(string(out))
	if strings.HasPrefix(trim, "[") {
		var fs []struct {
			File  string `json:"unit_file"`
			State string `json:"state"`
		}
		if err := json.Unmarshal([]byte(trim), &fs); err != nil {
			return nil, fmt.Errorf("unreadable systemctl output: %w", err)
		}
		for _, f := range fs {
			m[f.File] = f.State
		}
		return m, nil
	}
	for _, line := range strings.Split(trim, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.Contains(f[0], ".") {
			m[f[0]] = f[1]
		}
	}
	return m, nil
}

// parseShow parses `systemctl show a b c` output (blocks separated by empty
// lines) into one property map per block. Repeated keys are joined by "\n".
func parseShow(out []byte) []map[string]string {
	var blocks []map[string]string
	cur := map[string]string{}
	flush := func() {
		if len(cur) > 0 {
			blocks = append(blocks, cur)
			cur = map[string]string{}
		}
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		k, v := line[:i], line[i+1:]
		if old, ok := cur[k]; ok {
			cur[k] = old + "\n" + v
		} else {
			cur[k] = v
		}
	}
	flush()
	return blocks
}

// parseUint reads a systemd counter; "[not set]", "" and the all-ones value
// (infinity / not available) give nil.
func parseUint(s string) *uint64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "[") {
		return nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil || v == ^uint64(0) {
		return nil
	}
	return &v
}

// parseUnixStamp reads "@1790871752" (systemctl --timestamp=unix) as unix
// seconds, 0 when unset.
func parseUnixStamp(s string) int64 {
	s = strings.TrimPrefix(strings.TrimSpace(s), "@")
	if s == "" || s == "n/a" {
		return 0
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// parseUSecStamp reads a *USec property made with --timestamp=unix.
func parseUSecStamp(s string) int64 { return parseUnixStamp(s) }

func fields(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	return strings.Fields(s)
}

// ---- unit state ----

// State is the simplified state the UI filters on.
const (
	StateRunning  = "running"
	StateFailed   = "failed"
	StateStopped  = "stopped"
	StateFinished = "finished"
)

func stateOf(active, sub string) string {
	switch active {
	case "failed":
		return StateFailed
	case "active", "reloading", "activating":
		if sub == "exited" {
			return StateFinished
		}
		return StateRunning
	}
	return StateStopped
}
