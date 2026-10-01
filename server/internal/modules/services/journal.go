package services

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strconv"
)

// parseJournal reads `journalctl -o json` output (one object per line).
func parseJournal(out []byte) []LogLine {
	lines := []LogLine{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var e struct {
			Realtime string          `json:"__REALTIME_TIMESTAMP"`
			Priority string          `json:"PRIORITY"`
			Message  json.RawMessage `json:"MESSAGE"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		us, _ := strconv.ParseInt(e.Realtime, 10, 64)
		pri := 6
		if v, err := strconv.Atoi(e.Priority); err == nil {
			pri = v
		}
		lines = append(lines, LogLine{Time: us / 1000, Priority: pri, Message: journalMessage(e.Message)})
	}
	return lines
}

// journalMessage handles MESSAGE being a string or, for binary data, an array of bytes.
func journalMessage(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b []byte
	var nums []int
	if json.Unmarshal(raw, &nums) == nil {
		for _, n := range nums {
			b = append(b, byte(n))
		}
		return string(bytes.ToValidUTF8(b, []byte("?")))
	}
	return ""
}
