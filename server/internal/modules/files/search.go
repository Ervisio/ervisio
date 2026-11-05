package files

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

type searchParams struct {
	Root       string `json:"root"`
	Query      string `json:"query"`
	MaxResults int    `json:"maxResults"`
}

var hSearch = streamHandler(doSearch)

func nameMatcher(q string) func(string) bool {
	q = strings.ToLower(q)
	if strings.ContainsAny(q, "*?[") {
		return func(n string) bool {
			ok, _ := path.Match(q, strings.ToLower(n))
			return ok
		}
	}
	return func(n string) bool { return strings.Contains(strings.ToLower(n), q) }
}

var skipSearch = map[string]bool{"/proc": true, "/sys": true, "/dev": true, "/run": true}

// searchLimit is the number of results to return: 200 by default, at most 5000.
func searchLimit(requested int) int {
	if requested <= 0 {
		return 200
	}
	return min(requested, 5000)
}

// checkSearchRoot validates the search request and returns the clean root.
func checkSearchRoot(p searchParams) (string, error) {
	root, err := cleanPath(p.Root)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Query) == "" {
		return "", rpc.Errorf(rpc.Invalid, "Type something to search for.")
	}
	if fi, err := os.Stat(root); err != nil {
		return "", err
	} else if !fi.IsDir() {
		return "", rpc.Errorf(rpc.Invalid, "%s is not a folder.", root)
	}
	return root, nil
}

// searchBatch collects found entries and sends them in groups.
type searchBatch struct {
	s         rpc.Stream
	entries   []Entry
	lastFlush time.Time
}

func (b *searchBatch) flush() error {
	if len(b.entries) == 0 {
		return nil
	}
	err := b.s.Send(map[string]any{"entries": b.entries})
	b.entries = []Entry{}
	b.lastFlush = time.Now()
	return err
}

// due reports whether the batch should be sent now.
func (b *searchBatch) due() bool {
	return len(b.entries) >= 50 || (len(b.entries) > 0 && time.Since(b.lastFlush) > 150*time.Millisecond)
}

func doSearch(ctx context.Context, p searchParams, s rpc.Stream) error {
	root, err := checkSearchRoot(p)
	if err != nil {
		return err
	}
	limit := searchLimit(p.MaxResults)
	match := nameMatcher(p.Query)
	b := &searchBatch{s: s, entries: []Entry{}, lastFlush: time.Now()}
	count := 0
	truncated := false
	walkErr := filepath.WalkDir(root, func(q string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() && skipSearch[q] {
			return fs.SkipDir
		}
		if q != root && match(d.Name()) {
			if fi, err := d.Info(); err == nil {
				e := makeEntry(filepath.Dir(q), fi)
				e.Path = q
				b.entries = append(b.entries, e)
				count++
				if count >= limit {
					truncated = true
					return fs.SkipAll
				}
			}
		}
		if b.due() {
			return b.flush()
		}
		return nil
	})
	if walkErr != nil && ctx.Err() == nil {
		return walkErr
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := b.flush(); err != nil {
		return err
	}
	return s.Send(map[string]any{"done": true, "count": count, "truncated": truncated})
}
