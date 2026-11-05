package logs

import (
	"context"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

const (
	followBuffer     = 512
	followMaxBatch   = 200
	followFlushEvery = 150 * time.Millisecond
)

// follow streams new entries (batched every ~150 ms as arrays) until the
// client closes the stream.
func follow(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	specs, f, err := followSetup(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := make(chan Entry, followBuffer)
	fw := &followers{cancel: cancel}
	fw.startJournals(ctx, specs, f, out)
	fw.startSources(ctx, specs, f, out)
	return sendBatches(ctx, s, out, fw)
}

func followSetup(c *rpc.Call) ([]spec, filter, error) {
	var p baseParams
	if err := c.Bind(&p); err != nil {
		return nil, filter{}, err
	}
	specs, err := resolveSpecs(p.Sources, p.Watchers, c.Admin)
	if err != nil {
		return nil, filter{}, err
	}
	f, err := newFilter(0, 0, p.Levels, p.Text)
	if err != nil {
		return nil, filter{}, err
	}
	if hasJournal(specs) && !c.Admin && !journalReadable() {
		return nil, filter{}, errNeedsAdmin()
	}
	return specs, f, nil
}

// followers runs one goroutine per source; the first failure cancels the rest.
type followers struct {
	wg     sync.WaitGroup
	once   sync.Once
	err    error
	cancel context.CancelFunc
}

func (fw *followers) fail(err error) {
	fw.once.Do(func() { fw.err = err; fw.cancel() })
}

func (fw *followers) run(fn func()) {
	fw.wg.Add(1)
	go func() {
		defer fw.wg.Done()
		fn()
	}()
}

// finished closes the returned channel once every follower has returned;
// fw.err is safe to read after that.
func (fw *followers) finished() <-chan struct{} {
	done := make(chan struct{})
	go func() { fw.wg.Wait(); close(done) }()
	return done
}

func (fw *followers) startJournals(ctx context.Context, specs []spec, f filter, out chan<- Entry) {
	for _, g := range journalGroups(specs) {
		fw.run(func() {
			err := followJournal(ctx, g, f, out)
			if ctx.Err() != nil {
				return
			}
			if err = journalErr(err); err != nil {
				fw.fail(err)
			}
		})
	}
}

func (fw *followers) startSources(ctx context.Context, specs []spec, f filter, out chan<- Entry) {
	for _, sp := range specs {
		switch sp.Kind {
		case "file":
			fw.run(func() {
				if err := followFile(ctx, sp, f, out); err != nil && ctx.Err() == nil && len(specs) == 1 {
					fw.fail(err)
				}
			})
		case "evt":
			fw.run(func() {
				if err := followEventLog(ctx, sp.Channel, f, out); err != nil && ctx.Err() == nil {
					fw.fail(err)
				}
			})
		}
	}
}

func followJournal(ctx context.Context, g jgroup, f filter, out chan<- Entry) error {
	args := journalArgs(g, f, noLimit, "-f", "-n", "0")
	return sys.Stream(ctx, "journalctl", args, func(line string) error {
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
}

// sendBatches forwards entries in batches until every follower is done or
// ctx ends, then returns the first follower error.
func sendBatches(ctx context.Context, s rpc.Stream, out <-chan Entry, fw *followers) error {
	done := fw.finished()
	tick := time.NewTicker(followFlushEvery)
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
			if len(batch) < followMaxBatch {
				continue
			}
			if err := flush(); err != nil {
				return err
			}
		case <-tick.C:
			if err := flush(); err != nil {
				return err
			}
		case <-done:
			_ = flush()
			return fw.err
		case <-ctx.Done():
			<-done
			return fw.err
		}
	}
}
