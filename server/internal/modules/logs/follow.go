package logs

import (
	"context"
	"sync"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
	"github.com/Fonlogen/LinuxAdmin/server/internal/sys"
)

// follow streams new entries (batched every ~150 ms as arrays) until the
// client closes the stream.
func follow(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p baseParams
	if err := c.Bind(&p); err != nil {
		return err
	}
	specs, err := resolveSpecs(p.Sources, p.Watchers, c.Admin)
	if err != nil {
		return err
	}
	f, err := newFilter(0, 0, p.Levels, p.Text)
	if err != nil {
		return err
	}
	if hasJournal(specs) && !c.Admin && !journalReadable() {
		return errNeedsAdmin()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := make(chan Entry, 512)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	fail := func(err error) {
		once.Do(func() { firstErr = err; cancel() })
	}
	for _, g := range journalGroups(specs) {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			args := journalArgs(g, f, noLimit, "-f", "-n", "0")
			err := sys.Stream(ctx, "journalctl", args, func(line string) error {
				e, ok := parseJournalLine(line)
				if !ok || !f.match(e) {
					return nil
				}
				select {
				case out <- *e:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			if ctx.Err() == nil {
				if err = journalErr(err); err != nil {
					fail(err)
				}
			}
		}()
	}
	for _, sp := range specs {
		if sp.Kind != "file" {
			continue
		}
		sp := sp
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := followFile(ctx, sp, f, out); err != nil && ctx.Err() == nil && len(specs) == 1 {
				fail(err)
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	var batch []Entry
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		b := batch
		batch = nil
		return s.Send(b)
	}
	for {
		select {
		case e := <-out:
			batch = append(batch, e)
			if len(batch) >= 200 {
				if err := flush(); err != nil {
					return err
				}
			}
		case <-tick.C:
			if err := flush(); err != nil {
				return err
			}
		case <-done:
			_ = flush()
			if firstErr != nil {
				return firstErr
			}
			return nil
		case <-ctx.Done():
			<-done
			if firstErr != nil {
				return firstErr
			}
			return nil
		}
	}
}
