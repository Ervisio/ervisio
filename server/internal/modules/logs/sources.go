package logs

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ervisio/ervisio/server/internal/sys"
)

// Source is one entry of the sources sidebar.
type Source struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // journal | unit | kernel | file | evt
	Group      string `json:"group"`
	Label      string `json:"label"`
	Hint       string `json:"hint,omitempty"`
	Path       string `json:"path,omitempty"`
	Unit       string `json:"unit,omitempty"`
	Count24h   int    `json:"count24h"`
	Errors24h  int    `json:"errors24h"`
	Size       int64  `json:"size,omitempty"`
	NeedsAdmin bool   `json:"needsAdmin,omitempty"`
	Format     string `json:"format,omitempty"`
	Notify     bool   `json:"notify,omitempty"`
}

// SourceGroup is a sidebar group.
type SourceGroup struct {
	ID      string   `json:"id"` // system | services | files | watchers
	Sources []Source `json:"sources"`
}

// SourcesResult is the answer of logs.sources.
type SourcesResult struct {
	Groups          []SourceGroup `json:"groups"`
	Total24h        int           `json:"total24h"`
	Errors24h       int           `json:"errors24h"`
	Approx          bool          `json:"approx,omitempty"`
	JournalReadable bool          `json:"journalReadable"`
}

const (
	sampleCap     = 100000
	topUnits      = 30
	countTailSize = 512 << 10
	countBudget   = 48 << 20
)

type unitStat struct{ n, errs int }

// sampleJournal counts the newest entries of the last 24 hours.
func sampleJournal(ctx context.Context) (total, errs, kernel, kernelErrs int, units map[string]*unitStat, capped bool, err error) {
	units = map[string]*unitStat{}
	since := time.Now().Add(-24 * time.Hour).UnixMicro()
	args := []string{"-o", "json", "--no-pager", "-q", "-r", "-n", strconv.Itoa(sampleCap),
		"--since=" + usArg(since), "--output-fields=_SYSTEMD_UNIT,PRIORITY,_TRANSPORT"}
	err = sys.Stream(ctx, "journalctl", args, func(line string) error {
		var j struct {
			Unit jstr `json:"_SYSTEMD_UNIT"`
			P    jstr `json:"PRIORITY"`
			T    jstr `json:"_TRANSPORT"`
		}
		if json.Unmarshal([]byte(line), &j) != nil {
			return nil
		}
		isErr := false
		if p, e := strconv.Atoi(string(j.P)); e == nil && p <= 3 {
			isErr = true
		}
		total++
		if isErr {
			errs++
		}
		if j.T == "kernel" {
			kernel++
			if isErr {
				kernelErrs++
			}
		}
		if u := string(j.Unit); strings.HasSuffix(u, ".service") {
			st := units[u]
			if st == nil {
				st = &unitStat{}
				units[u] = st
			}
			st.n++
			if isErr {
				st.errs++
			}
		}
		return nil
	})
	capped = total >= sampleCap
	return
}

// countFile counts the entries of the last 24 hours in the tail of a file.
func countFile(ctx context.Context, path, format string, budget *int64) (n, errs int) {
	if atomic.AddInt64(budget, -countTailSize) < 0 {
		return 0, 0
	}
	since := time.Now().Add(-24 * time.Hour).UnixMicro()
	_ = scanFile(ctx, path, "", format, countTailSize, since, func(e *Entry) bool {
		n++
		if e.Level == LvErr {
			errs++
		}
		return true
	})
	return
}

func (w Watcher) source() Source {
	name := w.Name
	if name == "" {
		name = labelForPath(w.Path)
	}
	return Source{ID: "file:" + w.Path, Kind: "file", Group: "watchers", Label: name, Hint: w.Path, Path: w.Path, Format: w.Format, Notify: w.Notify}
}

func listSources(ctx context.Context, watchers []Watcher, admin bool) (*SourcesResult, error) {
	if onWindows {
		return listSourcesWindows(ctx, watchers, admin)
	}
	watchers = validWatchers(watchersOrPrefs(watchers))
	res := &SourcesResult{JournalReadable: admin || journalReadable()}
	system := SourceGroup{ID: "system"}
	services := SourceGroup{ID: "services", Sources: []Source{}}
	filesG := SourceGroup{ID: "files", Sources: []Source{}}
	watchG := SourceGroup{ID: "watchers", Sources: []Source{}}

	journal := Source{ID: "journal", Kind: "journal", Group: "system", Label: "journal", Hint: "systemd journal", NeedsAdmin: !res.JournalReadable}
	kernel := Source{ID: "kernel", Kind: "kernel", Group: "system", Label: "kernel", Hint: "journalctl -k", NeedsAdmin: !res.JournalReadable}
	boot := Source{ID: "boot", Kind: "journal", Group: "system", Label: "boot", Hint: "this boot only", NeedsAdmin: !res.JournalReadable}

	var wg sync.WaitGroup
	if res.JournalReadable {
		wg.Add(1)
		go func() {
			defer wg.Done()
			total, errs, kn, kerrs, units, capped, err := sampleJournal(ctx)
			if err != nil {
				return
			}
			res.Total24h, res.Errors24h, res.Approx = total, errs, capped
			journal.Count24h, journal.Errors24h = total, errs
			kernel.Count24h, kernel.Errors24h = kn, kerrs
			names := make([]string, 0, len(units))
			for u := range units {
				names = append(names, u)
			}
			sort.Slice(names, func(i, j int) bool {
				a, b := units[names[i]], units[names[j]]
				if a.n != b.n {
					return a.n > b.n
				}
				return names[i] < names[j]
			})
			if len(names) > topUnits {
				names = names[:topUnits]
			}
			for _, u := range names {
				services.Sources = append(services.Sources, Source{
					ID: "unit:" + u, Kind: "unit", Group: "services", Label: strings.TrimSuffix(u, ".service"),
					Hint: "journal: " + u, Unit: u, Count24h: units[u].n, Errors24h: units[u].errs,
				})
			}
		}()
	}

	budget := int64(countBudget)
	var fmu sync.Mutex
	addFile := func(group *SourceGroup, s Source, readable bool, format string) {
		defer wg.Done()
		if readable {
			s.Count24h, s.Errors24h = countFile(ctx, s.Path, format, &budget)
		}
		fmu.Lock()
		group.Sources = append(group.Sources, s)
		fmu.Unlock()
	}
	skip := map[string]bool{}
	for _, w := range watchers {
		skip[w.Path] = true
		s := w.source()
		readable := true
		if st, err := os.Stat(w.Path); err != nil {
			readable = false
			s.NeedsAdmin = os.IsPermission(err)
		} else {
			s.Size = st.Size()
			if fh, err := os.Open(w.Path); err != nil {
				readable = false
				s.NeedsAdmin = os.IsPermission(err)
			} else {
				fh.Close()
			}
		}
		wg.Add(1)
		go addFile(&watchG, s, readable, w.Format)
	}
	for _, lf := range listLogFiles("/var/log", 150) {
		if skip[lf.Path] {
			continue
		}
		s := Source{ID: "file:" + lf.Path, Kind: "file", Group: "files", Label: labelForPath(lf.Path),
			Hint: lf.Path, Path: lf.Path, Size: lf.Size, NeedsAdmin: lf.NeedsAdmin && !admin}
		wg.Add(1)
		go addFile(&filesG, s, !s.NeedsAdmin, "auto")
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	system.Sources = []Source{journal, kernel, boot}
	byLabel := func(g *SourceGroup, byCount bool) {
		sort.Slice(g.Sources, func(i, j int) bool {
			a, b := g.Sources[i], g.Sources[j]
			if byCount && a.Count24h != b.Count24h {
				return a.Count24h > b.Count24h
			}
			return a.Label < b.Label
		})
	}
	byLabel(&services, true)
	byLabel(&filesG, false)
	byLabel(&watchG, false)
	res.Groups = []SourceGroup{system, services, filesG, watchG}
	return res, nil
}
