package remote

import (
	"context"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

type stalledMediaFS struct{ stalledReadFS }

func (f *stalledMediaFS) Stat(context.Context, string) (Entry, error) {
	return Entry{Name: "clip.mp4", Type: "file", Size: 12, MTime: 1}, nil
}

func TestMediaCancellationClosesOnlyOwnedTransport(t *testing.T) {
	st := &store{dir: t.TempDir()}
	const id = "0123456789abcdef"
	if err := st.put(&Conn{ID: id, Name: "fixture", Kind: KindSFTP, Host: "fixture", Port: 22, User: "fixture", Auth: "password"}); err != nil {
		t.Fatal(err)
	}
	m := &module{st: st, pool: newPool(st)}
	stalled := &stalledMediaFS{stalledReadFS{closed: make(chan struct{})}}
	m.pool.dial = func(context.Context, *Conn) (FS, error) { return stalled, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- m.hMediaStream(ctx, remoteCall(map[string]any{"conn": id, "path": "/clip.mp4"}), &remoteTestStream{})
	}()
	select {
	case err := <-done:
		if !rpc.IsCode(err, rpc.Unavailable) {
			t.Fatalf("cancelled media: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Cancelled media held a stalled reader")
	}
	if len(m.pool.entries) != 0 || m.pool.open != 0 || len(mediaConnections) != 0 {
		t.Fatal("Media occupied persistent transfers or leaked a slot")
	}
}
