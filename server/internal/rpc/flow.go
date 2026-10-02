package rpc

import (
	"context"
	"encoding/json"
	"sync"
)

// inbox buffers up to Window messages for a consumer and acknowledges them to
// the peer as the consumer takes them.
type inbox[T any] struct {
	in  chan T
	out chan T
	ack func(n int)
	// mu orders push and close: a stream cancelled by its caller closes the
	// inbox while the read loop may still be delivering a message to it.
	mu     sync.Mutex
	closed bool
}

func newInbox[T any](ctx context.Context, ack func(n int)) *inbox[T] {
	b := &inbox[T]{in: make(chan T, Window), out: make(chan T), ack: ack}
	go b.forward(ctx)
	return b
}

// push queues v without blocking. It returns false when the peer exceeded
// the window (a protocol violation).
func (b *inbox[T]) push(v T) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return true // the consumer is gone: drop it
	}
	select {
	case b.in <- v:
		return true
	default:
		return false
	}
}

// close ends the inbox; queued messages are still delivered first.
func (b *inbox[T]) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.in)
	}
}

func (b *inbox[T]) forward(ctx context.Context) {
	defer close(b.out)
	pending := 0
	flush := func() {
		if pending > 0 {
			b.ack(pending)
			pending = 0
		}
	}
	for {
		var v T
		var ok bool
		select {
		case v, ok = <-b.in:
		case <-ctx.Done():
			return
		}
		if !ok {
			return
		}
		select {
		case b.out <- v:
		case <-ctx.Done():
			return
		}
		pending++
		if pending >= Window/4 || len(b.in) == 0 {
			flush()
		}
	}
}

// credits limits how many messages may be sent before the peer acknowledges.
type credits struct{ ch chan struct{} }

func newCredits() *credits { return &credits{ch: make(chan struct{}, Window)} }

func (c *credits) acquire(ctx context.Context) error {
	select {
	case c.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *credits) release(n int) {
	for ; n > 0; n-- {
		select {
		case <-c.ch:
		default:
			return
		}
	}
}

// rawOrNull returns m, or JSON null when m is empty.
func rawOrNull(m json.RawMessage) json.RawMessage {
	if len(m) == 0 {
		return json.RawMessage("null")
	}
	return m
}
