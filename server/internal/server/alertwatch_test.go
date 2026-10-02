package server

import "testing"

func TestAlertWatchSendsNewAndWorsenedOnly(t *testing.T) {
	w := newAlertWatch()
	a := func(id, sev string) watchedAlert { return watchedAlert{ID: id, Severity: sev, Title: id} }
	ids := func(l []watchedAlert) string {
		s := ""
		for _, x := range l {
			s += x.ID + ","
		}
		return s
	}
	// The first scan only records.
	if got := w.fresh([]watchedAlert{a("unit:x", "err"), a("updates", "warn")}); len(got) != 0 {
		t.Fatalf("first scan sent %v", got)
	}
	// Same alerts: nothing new. An ok alert is never sent.
	if got := w.fresh([]watchedAlert{a("unit:x", "err"), a("updates", "warn"), a("ssh", "ok")}); len(got) != 0 {
		t.Fatalf("unchanged sent %v", got)
	}
	// A new one and a warning that became an error.
	got := w.fresh([]watchedAlert{a("unit:x", "err"), a("updates", "err"), a("disk:/", "warn")})
	if ids(got) != "updates,disk:/," && ids(got) != "disk:/,updates," {
		t.Fatalf("new/worse: %s", ids(got))
	}
	// It goes away, then comes back: sent again.
	w.fresh(nil)
	if got := w.fresh([]watchedAlert{a("unit:x", "err")}); ids(got) != "unit:x," {
		t.Fatalf("came back: %s", ids(got))
	}
	// An error that improves to a warning is not sent again.
	if got := w.fresh([]watchedAlert{a("unit:x", "warn")}); len(got) != 0 {
		t.Fatalf("improved: %v", got)
	}
}
