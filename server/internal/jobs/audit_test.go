package jobs

import (
	"strings"
	"testing"

	"github.com/ervisio/ervisio/server/internal/audit"
)

type auditCfg struct{}

func (auditCfg) AuditEnabled() bool      { return true }
func (auditCfg) AuditRetentionDays() int { return 30 }

func TestJobsRecordToTheActivityLog(t *testing.T) {
	l := audit.New(t.TempDir(), auditCfg{})
	audit.SetDefault(l)
	defer audit.ClearDefault(l)

	// A job that needs administrator rights: the approval is recorded.
	f := newFixture(t)
	v := f.approved("alice", "rooty", nil, nil)
	f.m.RunNow(f.caller("alice"), "jt", v.ID)
	f.m.Wait()

	// A webhook call.
	f2, v2, token := hookFixture(t)
	if r := f2.m.HandleHook("jt", token, "1.1.1.1", nil, nil); r.Status != 202 {
		t.Fatalf("hook: %+v", r)
	}
	f2.m.Wait()

	all, _, err := l.List(audit.Query{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	has := func(action, origin, user string) *audit.Entry {
		for i := range all {
			e := &all[i]
			if e.Action == action && strings.HasPrefix(e.Origin, origin) && e.User == user {
				return e
			}
		}
		return nil
	}
	if has("job.approve", "", "alice") == nil {
		t.Errorf("no approval entry: %+v", all)
	}
	if e := has("job.run", "job ", "alice"); e == nil || e.Plugin != "jt" {
		t.Errorf("no run entry: %+v", all)
	}
	if e := has("command", "job ", "alice"); e == nil || e.Source != audit.SourcePlugin {
		t.Errorf("no command entry: %+v", all)
	}
	if has("job.webhook", "webhook", v2.Owner) == nil || has("job.run", "webhook", v2.Owner) == nil {
		t.Errorf("no webhook entries: %+v", all)
	}
}
