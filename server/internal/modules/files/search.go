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

func hSearch(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p searchParams
	if err := c.Bind(&p); err != nil {
		return err
	}
	return doSearch(ctx, p, s)
}

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

func doSearch(ctx context.Context, p searchParams, s rpc.Stream) error {
	root, err := cleanPath(p.Root)
	if err != nil {
		return err
	}
	if strings.TrimSpace(p.Query) == "" {
		return rpc.Errorf(rpc.Invalid, "Type something to search for.")
	}
	if fi, err := os.Stat(root); err != nil {
		return err
	} else if !fi.IsDir() {
		return rpc.Errorf(rpc.Invalid, "%s is not a folder.", root)
	}
	max := p.MaxResults
	if max <= 0 {
		max = 200
	}
	if max > 5000 {
		max = 5000
	}
	match := nameMatcher(p.Query)
	batch := []Entry{}
	count := 0
	lastFlush := time.Now()
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := s.Send(map[string]any{"entries": batch})
		batch = []Entry{}
		lastFlush = time.Now()
		return err
	}
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
				batch = append(batch, e)
				count++
				if count >= max {
					truncated = true
					return fs.SkipAll
				}
			}
		}
		if len(batch) >= 50 || (len(batch) > 0 && time.Since(lastFlush) > 150*time.Millisecond) {
			return flush()
		}
		return nil
	})
	if walkErr != nil && ctx.Err() == nil {
		return walkErr
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := flush(); err != nil {
		return err
	}
	return s.Send(map[string]any{"done": true, "count": count, "truncated": truncated})
}
