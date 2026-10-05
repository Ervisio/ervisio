package software

import (
	"context"
	"encoding/xml"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// Windows scheduling uses one Task Scheduler task running as SYSTEM. The
// task definition is rendered as XML (platform-neutral, so it is testable
// everywhere) and registered with `schtasks /Create /XML`.
const (
	taskFolder = `\` + brand.Name
	taskName   = taskFolder + `\SoftwareUpdate`
)

// taskAction is one <Exec> of the task: a program and its arguments.
type taskAction struct {
	Path string
	Args []string
}

// winQuote quotes one argument following the CommandLineToArgvW rules.
func winQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	bs := 0
	for _, r := range s {
		switch r {
		case '\\':
			bs++
			b.WriteRune(r)
			continue
		case '"':
			b.WriteString(strings.Repeat(`\`, bs+1))
		}
		bs = 0
		b.WriteRune(r)
	}
	b.WriteString(strings.Repeat(`\`, bs)) // backslashes before the closing quote
	b.WriteByte('"')
	return b.String()
}

func winArgs(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = winQuote(a)
	}
	return strings.Join(q, " ")
}

func xmlEsc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// renderTaskXML builds the task: daily at "HH:MM" local time, as SYSTEM,
// catching up after a missed start (like Persistent=true).
func renderTaskXML(at string, actions []taskAction) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-16"?>` + "\n")
	b.WriteString(`<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">` + "\n")
	b.WriteString("  <RegistrationInfo><Description>Written by " + xmlEsc(brand.Name) + " (Software &gt; Update all &gt; schedule). Remove it from the web UI.</Description></RegistrationInfo>\n")
	b.WriteString("  <Triggers><CalendarTrigger><StartBoundary>2024-01-01T" + at + ":00</StartBoundary><Enabled>true</Enabled>" +
		"<ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay></CalendarTrigger></Triggers>\n")
	b.WriteString("  <Principals><Principal id=\"Author\"><UserId>S-1-5-18</UserId><RunLevel>HighestAvailable</RunLevel></Principal></Principals>\n")
	b.WriteString("  <Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>" +
		"<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable>" +
		"<RunOnlyIfNetworkAvailable>true</RunOnlyIfNetworkAvailable><ExecutionTimeLimit>PT4H</ExecutionTimeLimit><Enabled>true</Enabled></Settings>\n")
	b.WriteString("  <Actions Context=\"Author\">\n")
	for _, a := range actions {
		b.WriteString("    <Exec><Command>" + xmlEsc(a.Path) + "</Command>")
		if len(a.Args) > 0 {
			b.WriteString("<Arguments>" + xmlEsc(winArgs(a.Args)) + "</Arguments>")
		}
		b.WriteString("</Exec>\n")
	}
	b.WriteString("  </Actions>\n</Task>\n")
	return b.String()
}

var startBoundaryRe = regexp.MustCompile(`<StartBoundary>[^<T]*T([01][0-9]|2[0-3]):([0-5][0-9])`)

// parseTaskTime extracts the daily time ("03:00") from a task XML as printed
// by `schtasks /Query /XML`, or "".
func parseTaskTime(x string) string {
	if g := startBoundaryRe.FindStringSubmatch(x); g != nil {
		return g[1] + ":" + g[2]
	}
	return ""
}

// encodeUTF16 returns s as UTF-16LE with a byte order mark (what schtasks
// expects for a file declaring encoding="UTF-16").
func encodeUTF16(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 0, 2+2*len(u))
	out = append(out, 0xFF, 0xFE)
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

// decodeText turns schtasks output into a string, accepting UTF-16 (with BOM)
// as well as plain bytes.
func decodeText(b []byte) string {
	if len(b) >= 2 && ((b[0] == 0xFF && b[1] == 0xFE) || (b[0] == 0xFE && b[1] == 0xFF)) {
		le := b[0] == 0xFF
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			if le {
				u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
			} else {
				u = append(u, uint16(b[i+1])|uint16(b[i])<<8)
			}
		}
		return string(utf16.Decode(u))
	}
	return strings.TrimPrefix(string(b), "\xef\xbb\xbf")
}

// isAccessDenied recognises schtasks' refusal for a non-administrator.
func isAccessDenied(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "access is denied") || strings.Contains(s, "0x80070005")
}

var (
	osUpdMu   sync.Mutex
	osUpdAt   time.Time
	osUpdLast = -1
)

// cachedOSUpdates returns the pending Windows Update count (-1 = unknown or
// not Windows), refreshed at most every 30 minutes because the search is slow.
func cachedOSUpdates(ctx context.Context) int {
	if runtime.GOOS != "windows" {
		return -1
	}
	osUpdMu.Lock()
	defer osUpdMu.Unlock()
	if time.Since(osUpdAt) > 30*time.Minute {
		osUpdLast, osUpdAt = osUpdatesCount(ctx), time.Now()
	}
	return osUpdLast
}
