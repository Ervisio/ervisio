package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/mail"
	"sort"
	"sync"
	"time"
)

func bareAddr(a string) (string, error) {
	p, err := mail.ParseAddress(a)
	if err != nil {
		return "", err
	}
	return p.Address, nil
}

func newID(list []Spec) string {
	for {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		id := hex.EncodeToString(b)
		clash := false
		for i := range list {
			clash = clash || list[i].ID == id
		}
		if !clash {
			return id
		}
	}
}

// Plugin rate limits (plugins.notify and job notify steps).
const (
	pluginPerMinute = 10
	pluginPerHour   = 60
)

// Service sends notifications and manages the channels file.
type Service struct {
	Store *Store
	// Now is the clock (tests); nil = time.Now.
	Now func() time.Time
	// Logf logs delivery failures (never secrets); may be nil.
	Logf func(format string, args ...any)

	mu   sync.Mutex
	last map[string]Result
	sent map[string][]time.Time
}

// New returns a Service over the channels file at path.
func New(path string) *Service {
	return &Service{Store: &Store{Path: path}, last: map[string]Result{}, sent: map[string][]time.Time{}}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) logf(f string, a ...any) {
	if s.Logf != nil {
		s.Logf(f, a...)
	}
}

// List returns the channels as the browser may see them.
func (s *Service) List() ([]Channel, error) {
	list, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Channel, 0, len(list))
	for i := range list {
		var last *Result
		if r, ok := s.last[list[i].ID]; ok {
			r := r
			last = &r
		}
		out = append(out, list[i].View(last))
	}
	return out, nil
}

// Save creates or replaces a channel and returns its browser view.
func (s *Service) Save(in Spec) (Channel, error) {
	sp, err := s.Store.Save(in)
	if err != nil {
		return Channel{}, err
	}
	return sp.View(nil), nil
}

// Delete removes a channel.
func (s *Service) Delete(id string) error {
	err := s.Store.Delete(id)
	if err == nil {
		s.mu.Lock()
		delete(s.last, id)
		s.mu.Unlock()
	}
	return err
}

// Test sends a test message through a channel as submitted (a draft, not
// yet saved, or a saved one with in.ID set: empty secrets then come from
// the stored channel). The error says what went wrong, without secrets.
func (s *Service) Test(ctx context.Context, in Spec) error {
	if in.ID != "" {
		old, err := s.Store.Get(in.ID)
		if err != nil {
			return err
		}
		in.keepSecrets(old)
	}
	in.Normalise()
	if err := in.Validate(); err != nil {
		return err
	}
	err := send(ctx, &in, Message{Title: "Ervisio test notification", Body: "If you can read this, the channel " + in.Name + " works.", Level: "info", Source: "Ervisio"}.Clean())
	if in.ID != "" {
		s.record(in.ID, err)
	}
	return err
}

func (s *Service) record(id string, err error) {
	r := Result{At: s.now().UnixMilli(), OK: err == nil}
	if err != nil {
		r.Error = err.Error()
	}
	s.mu.Lock()
	s.last[id] = r
	s.mu.Unlock()
}

// Delivery is the outcome of Send.
type Delivery struct {
	// Channels is how many channels matched the message.
	Channels  int `json:"channels"`
	Delivered int `json:"delivered"`
	Failed    int `json:"failed"`
}

// Send delivers a message to every enabled channel that subscribes to the
// event and whose minimum level it reaches. Failures are logged and kept
// as the channel's last result; they never reach the sender, which only
// gets the counts.
func (s *Service) Send(ctx context.Context, msg Message, event string) Delivery {
	msg = msg.Clean()
	if msg.Source == "" {
		msg.Source = "Ervisio"
	}
	list, err := s.Store.Load()
	if err != nil {
		s.logf("notify: %v", err)
		return Delivery{}
	}
	var targets []Spec
	for _, c := range list {
		if !c.Enabled || levelRank[msg.Level] < levelRank[c.minLevel()] {
			continue
		}
		ok := false
		for _, e := range c.Events {
			ok = ok || e == event
		}
		if ok {
			targets = append(targets, c)
		}
	}
	d := Delivery{Channels: len(targets)}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range targets {
		wg.Add(1)
		go func(c *Spec) {
			defer wg.Done()
			err := send(ctx, c, msg)
			s.record(c.ID, err)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				d.Failed++
				s.logf("notify: channel %q (%s) failed: %v", c.Name, c.Type, err)
			} else {
				d.Delivered++
			}
		}(&targets[i])
	}
	wg.Wait()
	return d
}

// Subscribed reports whether an enabled channel wants the event.
func (s *Service) Subscribed(event string) bool {
	list, err := s.Store.Load()
	if err != nil {
		return false
	}
	for _, c := range list {
		if !c.Enabled {
			continue
		}
		for _, e := range c.Events {
			if e == event {
				return true
			}
		}
	}
	return false
}

// AllowPlugin counts one notification of a plugin against its rate limit
// (10 a minute, 60 an hour). It returns false and how long to wait when the
// plugin is over it.
func (s *Service) AllowPlugin(plugin string) (bool, time.Duration) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	times := s.sent[plugin]
	keep := times[:0]
	for _, t := range times {
		if now.Sub(t) < time.Hour {
			keep = append(keep, t)
		}
	}
	times = keep
	inMinute := 0
	for _, t := range times {
		if now.Sub(t) < time.Minute {
			inMinute++
		}
	}
	if inMinute >= pluginPerMinute || len(times) >= pluginPerHour {
		var wait time.Duration
		if inMinute >= pluginPerMinute {
			wait = time.Minute - now.Sub(times[len(times)-inMinute])
		} else {
			wait = time.Hour - now.Sub(times[0])
		}
		s.sent[plugin] = times
		return false, wait
	}
	s.sent[plugin] = append(times, now)
	// Forget plugins that stopped sending.
	if len(s.sent) > 200 {
		keys := make([]string, 0, len(s.sent))
		for k := range s.sent {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys[:len(keys)-100] {
			delete(s.sent, k)
		}
	}
	return true, 0
}
