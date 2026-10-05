package logs

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// Windows Event Log backend. It runs `wevtutil qe` and parses the
// RenderedXml output; the code is platform neutral (so it is testable on
// Linux) but is only reachable when onWindows is true.

// eventChannels are the channels offered as sources.
var eventChannels = []string{
	"Application",
	"System",
	"Security",
	"Setup",
	"Microsoft-Windows-PowerShell/Operational",
}

var channelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _./-]{0,199}$`)

const (
	histEventCap    = 50000
	evtTextScan     = 5000
	evtPollInterval = 2 * time.Second
	evtMaxOutput    = 64 << 20
)

type evtXML struct {
	System struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		EventID     string `xml:"EventID"`
		Level       string `xml:"Level"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
		RecordID  int64 `xml:"EventRecordID"`
		Execution struct {
			PID int `xml:"ProcessID,attr"`
		} `xml:"Execution"`
		Channel string `xml:"Channel"`
	} `xml:"System"`
	EventData struct {
		Data []struct {
			Name  string `xml:"Name,attr"`
			Value string `xml:",chardata"`
		} `xml:"Data"`
	} `xml:"EventData"`
	UserData struct {
		Inner string `xml:",innerxml"`
	} `xml:"UserData"`
	Rendering struct {
		Message string `xml:"Message"`
	} `xml:"RenderingInfo"`
}

// evtLevel maps a Windows event level onto the API level names.
func evtLevel(l int) string {
	switch l {
	case 1, 2:
		return LvErr
	case 3:
		return LvWarn
	case 5:
		return LvDebug
	}
	return LvInfo // 0 (LogAlways), 4 and unknown
}

// entryFromEvent converts a decoded <Event>.
func entryFromEvent(ev *evtXML, channel string) (*Entry, bool) {
	ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(ev.System.TimeCreated.SystemTime))
	if err != nil || ev.System.RecordID <= 0 {
		return nil, false
	}
	lvl, _ := strconv.Atoi(strings.TrimSpace(ev.System.Level))
	msg := strings.TrimSpace(ev.Rendering.Message)
	if msg == "" {
		var parts []string
		for _, d := range ev.EventData.Data {
			v := strings.TrimSpace(d.Value)
			if v == "" {
				continue
			}
			if d.Name != "" {
				v = d.Name + "=" + v
			}
			parts = append(parts, v)
		}
		msg = strings.Join(parts, " ")
	}
	if id := strings.TrimSpace(ev.System.EventID); id != "" {
		if msg == "" {
			msg = "Event " + id
		}
	}
	prov := ev.System.Provider.Name
	src := prov
	if src == "" {
		src = channel
	}
	us := ts.UnixMicro()
	return &Entry{
		Ts: us / 1000, TsUs: us, Level: evtLevel(lvl), Source: src, SrcID: "evt:" + channel,
		Message: clip(msg), Unit: prov, Pid: ev.System.Execution.PID,
		Cursor: channel + "/" + strconv.FormatInt(ev.System.RecordID, 10),
	}, true
}

// parseEventStream decodes the concatenated <Event> elements wevtutil prints
// and returns the entries with their record ids, in input order.
func parseEventStream(data []byte, channel string) ([]Entry, []int64) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	var ents []Entry
	var ids []int64
	for {
		tok, err := dec.Token()
		if err != nil {
			break // io.EOF or garbage at the end
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Event" {
			continue
		}
		var ev evtXML
		if err := dec.DecodeElement(&ev, &se); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			continue
		}
		if e, ok := entryFromEvent(&ev, channel); ok {
			ents = append(ents, *e)
			ids = append(ids, ev.System.RecordID)
		}
	}
	return ents, ids
}

func evtTime(us int64) string {
	return time.UnixMicro(us).UTC().Format("2006-01-02T15:04:05.000Z")
}

// levelXPath returns the Level condition for the allowed levels ("" = all).
func levelXPath(levels map[string]bool) string {
	if levels == nil {
		return ""
	}
	var c []string
	if levels[LvErr] {
		c = append(c, "Level=1", "Level=2")
	}
	if levels[LvWarn] {
		c = append(c, "Level=3")
	}
	if levels[LvInfo] {
		c = append(c, "Level=0", "Level=4")
	}
	if levels[LvDebug] {
		c = append(c, "Level=5")
	}
	if len(c) == 0 || len(c) == 6 {
		return ""
	}
	return "(" + strings.Join(c, " or ") + ")"
}

// eventQuery builds the XPath query; afterID > 0 adds EventRecordID > afterID.
func eventQuery(f filter, until int64, afterID int64) string {
	var conds []string
	if f.sinceUs > 0 {
		conds = append(conds, "TimeCreated[@SystemTime>='"+evtTime(f.sinceUs)+"']")
	}
	if until != noLimit {
		// wevtutil compares at ms precision.
		conds = append(conds, "TimeCreated[@SystemTime<='"+evtTime(until)+"']")
	}
	if l := levelXPath(f.levels); l != "" {
		conds = append(conds, l)
	}
	if afterID > 0 {
		conds = append(conds, "EventRecordID>"+strconv.FormatInt(afterID, 10))
	}
	if len(conds) == 0 {
		return "*"
	}
	return "*[System[" + strings.Join(conds, " and ") + "]]"
}

func evtArgs(channel, query string, count int, newestFirst bool) []string {
	return []string{"qe", channel, "/rd:" + strconv.FormatBool(newestFirst), "/c:" + strconv.Itoa(count),
		"/f:RenderedXml", "/q:" + query}
}

func evtErr(err error) error {
	if err == nil {
		return nil
	}
	var ee *sys.ExitError
	if errors.As(err, &ee) {
		low := strings.ToLower(ee.Stderr)
		if ee.Code == 5 || strings.Contains(low, "access is denied") {
			return rpc.Errorf(rpc.NeedsAdmin, "Reading this Windows event log needs administrator rights.")
		}
		if strings.Contains(low, "channel") && strings.Contains(low, "not found") {
			return rpc.Errorf(rpc.NotFound, "the event log %s is not available", strings.TrimSpace(ee.Stderr))
		}
	}
	return err
}

func runWevtutil(ctx context.Context, args []string) ([]byte, error) {
	out, err := sys.Cmd{Name: "wevtutil", Args: args, MaxOutput: evtMaxOutput}.Output(ctx)
	return out, evtErr(err)
}

// fetchEventLog returns up to want matching entries with TsUs <= until, newest first.
func fetchEventLog(ctx context.Context, channel string, f filter, until int64, want int) ([]Entry, error) {
	n := want*2 + 50
	if f.text != "" && n < evtTextScan {
		n = evtTextScan
	}
	out, err := runWevtutil(ctx, evtArgs(channel, eventQuery(f, until, 0), n, true))
	if err != nil {
		return nil, err
	}
	ents, _ := parseEventStream(out, channel)
	res := ents[:0]
	for _, e := range ents {
		if e.TsUs > until || !f.match(&e) {
			continue
		}
		res = append(res, e)
		if len(res) >= want {
			break
		}
	}
	return res, nil
}

// followEventLog polls the channel for records newer than the current newest.
func followEventLog(ctx context.Context, channel string, f filter, out chan<- Entry) error {
	var last int64
	o, err := runWevtutil(ctx, evtArgs(channel, "*", 1, true))
	if err != nil {
		return err
	}
	if _, ids := parseEventStream(o, channel); len(ids) > 0 {
		last = ids[0]
	}
	t := time.NewTicker(evtPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		o, err := runWevtutil(ctx, evtArgs(channel, eventQuery(filter{levels: f.levels, untilUs: noLimit}, noLimit, last), 1000, false))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		ents, ids := parseEventStream(o, channel)
		for i, e := range ents {
			if ids[i] > last {
				last = ids[i]
			}
			if !f.match(&e) {
				continue
			}
			select {
			case out <- e:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

// listSourcesWindows is logs.sources on Windows: the event channels and the
// watched files.
func listSourcesWindows(ctx context.Context, watchers []Watcher, admin bool) (*SourcesResult, error) {
	watchers = validWatchers(watchersOrPrefs(watchers))
	res := &SourcesResult{JournalReadable: true}
	system := SourceGroup{ID: "system", Sources: make([]Source, len(eventChannels))}
	watchG := SourceGroup{ID: "watchers", Sources: []Source{}}
	since := time.Now().Add(-24 * time.Hour).UnixMicro()
	var wg sync.WaitGroup
	var capped atomic.Bool
	for i, ch := range eventChannels {
		label := ch
		if j := strings.LastIndexByte(label, '/'); j >= 0 {
			label = label[:j]
		}
		label = strings.TrimPrefix(label, "Microsoft-Windows-")
		s := Source{ID: "evt:" + ch, Kind: "evt", Group: "system", Label: label, Hint: "Windows Event Log: " + ch}
		system.Sources[i] = s
		wg.Add(1)
		go func(i int, ch string) {
			defer wg.Done()
			ents, err := fetchEventLog(ctx, ch, filter{sinceUs: since, untilUs: noLimit}, noLimit, sampleCap/10)
			if err != nil {
				system.Sources[i].NeedsAdmin = rpc.ToError(err, admin).Code == rpc.NeedsAdmin
				return
			}
			if len(ents) >= sampleCap/10 {
				capped.Store(true)
			}
			for _, e := range ents {
				system.Sources[i].Count24h++
				if e.Level == LvErr {
					system.Sources[i].Errors24h++
				}
			}
		}(i, ch)
	}
	budget := int64(countBudget)
	var mu sync.Mutex
	for _, w := range watchers {
		s := w.source()
		wg.Add(1)
		go func(w Watcher, s Source) {
			defer wg.Done()
			if st, err := os.Stat(w.Path); err != nil {
				s.NeedsAdmin = os.IsPermission(err)
			} else {
				s.Size = st.Size()
				s.Count24h, s.Errors24h = countFile(ctx, w.Path, w.Format, &budget)
			}
			mu.Lock()
			watchG.Sources = append(watchG.Sources, s)
			mu.Unlock()
		}(w, s)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, s := range system.Sources {
		res.Total24h += s.Count24h
		res.Errors24h += s.Errors24h
	}
	res.Approx = capped.Load()
	sort.Slice(watchG.Sources, func(i, j int) bool { return watchG.Sources[i].Label < watchG.Sources[j].Label })
	res.Groups = []SourceGroup{system, {ID: "services", Sources: []Source{}}, {ID: "files", Sources: []Source{}}, watchG}
	return res, nil
}
