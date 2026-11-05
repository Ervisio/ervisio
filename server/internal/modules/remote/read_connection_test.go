package remote

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

type stalledReadFS struct {
	FS
	closed chan struct{}
	once   sync.Once
}

func (f *stalledReadFS) Close() error { f.once.Do(func() { close(f.closed) }); return nil }
func (f *stalledReadFS) List(context.Context, string) ([]Entry, error) {
	return []Entry{{Name: "stalled.txt", Type: "file", Size: 12}}, nil
}
func (f *stalledReadFS) Open(context.Context, string) (io.ReadCloser, error) {
	return stalledReader{f}, nil
}

type stalledReader struct{ fs *stalledReadFS }

func (r stalledReader) Read([]byte) (int, error) { <-r.fs.closed; return 0, io.EOF }
func (r stalledReader) Close() error             { return nil }

func TestRemoteProjectSearchCancellationInterruptsOwnedReaderWithoutPoolChanges(t *testing.T) {
	st := &store{dir: t.TempDir()}
	const id = "0123456789abcdef"
	if err := st.put(&Conn{ID: id, Name: "stalled fixture", Kind: KindSFTP, Host: "fixture", Port: 22, User: "fixture", Auth: "password"}); err != nil {
		t.Fatal(err)
	}
	m := &module{st: st, pool: newPool(st)}
	stalled := &stalledReadFS{closed: make(chan struct{})}
	m.pool.dial = func(context.Context, *Conn) (FS, error) { return stalled, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- m.hSearchText(ctx, remoteCall(map[string]any{"conn": id, "root": "/", "query": "hello"}), &remoteTestStream{})
	}()
	select {
	case err := <-done:
		if !rpc.IsCode(err, rpc.Unavailable) {
			t.Fatalf("cancelled search returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not interrupt a blocked remote reader")
	}
	if len(m.pool.entries) != 0 || m.pool.open != 0 {
		t.Fatal("read-only search occupied persistent transfer slots")
	}
}
