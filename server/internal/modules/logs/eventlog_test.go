package logs

import (
	"strings"
	"testing"
)

const sampleRendered = `<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><Provider Name='Service Control Manager' Guid='{555908d1-a6d7-4695-8e1e-26931d2012f4}' EventSourceName='Service Control Manager'/><EventID Qualifiers='49152'>7000</EventID><Level>2</Level><TimeCreated SystemTime='2026-10-05T08:15:30.123456700Z'/><EventRecordID>4321</EventRecordID><Execution ProcessID='700' ThreadID='912'/><Channel>System</Channel><Computer>HOST</Computer></System><EventData><Data Name='param1'>Foo Service</Data></EventData><RenderingInfo Culture='en-US'><Message>The Foo Service failed to start &amp; exited.</Message><Level>Error</Level></RenderingInfo></Event>
<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><Provider Name='Contoso'/><EventID>1</EventID><Level>0</Level><TimeCreated SystemTime='2026-10-05T08:15:31.000Z'/><EventRecordID>4322</EventRecordID><Execution ProcessID='0' ThreadID='0'/><Channel>System</Channel></System><EventData><Data Name='a'>x</Data><Data>y</Data></EventData></Event>
`

func TestParseEventStream(t *testing.T) {
	ents, ids := parseEventStream([]byte(sampleRendered), "System")
	if len(ents) != 2 || ids[0] != 4321 || ids[1] != 4322 {
		t.Fatalf("got %+v %v", ents, ids)
	}
	e := ents[0]
	if e.Level != LvErr || e.Source != "Service Control Manager" || e.Unit != e.Source || e.Pid != 700 ||
		e.Message != "The Foo Service failed to start & exited." || e.Cursor != "System/4321" || e.SrcID != "evt:System" {
		t.Errorf("entry 0: %+v", e)
	}
	if e.TsUs != 1791188130123456 || e.Ts != 1791188130123 {
		t.Errorf("ts: %d %d", e.Ts, e.TsUs)
	}
	if ents[1].Level != LvInfo || ents[1].Message != "a=x y" {
		t.Errorf("entry 1: %+v", ents[1])
	}
}

func TestEvtLevel(t *testing.T) {
	for l, want := range map[int]string{0: LvInfo, 1: LvErr, 2: LvErr, 3: LvWarn, 4: LvInfo, 5: LvDebug, 9: LvInfo} {
		if got := evtLevel(l); got != want {
			t.Errorf("level %d = %s, want %s", l, got, want)
		}
	}
}

func TestEventQuery(t *testing.T) {
	f, _ := newFilter(1790000000000, 1790000100000, []string{"err", "warn"}, "")
	q := eventQuery(f, f.untilUs, 0)
	for _, s := range []string{"@SystemTime>='2026-", "@SystemTime<='2026-", "(Level=1 or Level=2 or Level=3)"} {
		if !strings.Contains(q, s) {
			t.Errorf("query %q lacks %q", q, s)
		}
	}
	if q := eventQuery(filter{untilUs: noLimit}, noLimit, 0); q != "*" {
		t.Errorf("empty query = %q", q)
	}
	if q := eventQuery(filter{untilUs: noLimit}, noLimit, 77); q != "*[System[EventRecordID>77]]" {
		t.Errorf("after query = %q", q)
	}
}
