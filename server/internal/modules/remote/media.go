package remote

import (
	"context"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Media has its own small connection budget. A cancelled seek may interrupt
// its transport, without closing a connection used by another transfer.
var mediaConnections = make(chan struct{}, 4)

func (m *module) hMediaStream(parent context.Context, c *rpc.Call, s rpc.Stream) error {
	var p struct {
		pathParams
		Offset       int64  `json:"offset"`
		Length       *int64 `json:"length"`
		MetadataOnly bool   `json:"metadataOnly"`
	}
	if err := c.Bind(&p); err != nil {
		return err
	}
	pth, err := cleanRemote(p.Path)
	if err != nil {
		return err
	}
	spec, err := m.st.get(p.Conn)
	if err != nil {
		return err
	}
	if spec.Kind != KindSFTP && spec.Kind != KindSMB {
		return rpc.Errorf(rpc.Unavailable, "Media preview requires a local, SFTP or SMB file. Download this file to open it locally.")
	}
	ctx, cancel := context.WithTimeout(parent, 6*time.Hour)
	defer cancel()
	select {
	case mediaConnections <- struct{}{}:
		defer func() { <-mediaConnections }()
	case <-ctx.Done():
		return ctx.Err()
	default:
		return rpc.Errorf(rpc.Unavailable, "Other media files are opening. Try again shortly.")
	}
	f, err := m.pool.dial(ctx, spec)
	if err != nil {
		return remoteErr(err)
	}
	var once sync.Once
	closeOwned := func() {
		once.Do(func() {
			switch actual := f.(type) {
			case *sftpFS:
				_ = actual.ssh.Close()
			case *smbFS:
				_ = actual.conn.Close()
			}
			_ = f.Close()
		})
	}
	stop := context.AfterFunc(ctx, closeOwned)
	defer stop()
	defer closeOwned()
	err = readStreamWindow(ctx, &handle{FS: f, spec: spec}, pth, s, p.Offset, p.Length, p.MetadataOnly)
	if ctx.Err() != nil {
		return rpc.Errorf(rpc.Unavailable, "The media transfer was cancelled.")
	}
	return remoteErr(err)
}
