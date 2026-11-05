package remote

import (
	"context"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Every bridge is one user process. Read-only overview/search connections have a
// separate bounded budget and cannot occupy or close persistent transfer slots.
var readConnectionSlots = make(chan struct{}, 4)

func (m *module) withReadConnection(parent context.Context, id string, run func(context.Context, FS) error) error {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	select {
	case readConnectionSlots <- struct{}{}:
		defer func() { <-readConnectionSlots }()
	case <-ctx.Done():
		return rpc.Errorf(rpc.Unavailable, "The remote read was cancelled.")
	default:
		return rpc.Errorf(rpc.Unavailable, "Other remote checks are running. Try again shortly.")
	}
	spec, err := m.st.get(id)
	if err != nil {
		return err
	}
	f, err := m.pool.dial(ctx, spec)
	if err != nil {
		return remoteErr(err)
	}
	var closeOnce sync.Once
	closeOwned := func() {
		closeOnce.Do(func() {
			// Interrupt transport before protocol cleanup; a stalled peer must not
			// hold a search after Stop or its deadline. This connection is not shared.
			switch remote := f.(type) {
			case *sftpFS:
				_ = remote.ssh.Close()
			case *smbFS:
				_ = remote.conn.Close()
			}
			_ = f.Close()
		})
	}
	stop := context.AfterFunc(ctx, closeOwned)
	defer stop()
	defer closeOwned()
	err = run(ctx, f)
	if ctx.Err() != nil {
		return rpc.Errorf(rpc.Unavailable, "The remote read was cancelled or reached its time limit.")
	}
	return remoteErr(err)
}
