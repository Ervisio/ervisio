package remote

import (
	"context"
	"github.com/ervisio/ervisio/server/internal/modules/files"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

func (m *module) hSearchText(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p struct {
		Conn string `json:"conn"`
		files.TextSearchParams
	}
	if err := c.Bind(&p); err != nil {
		return err
	}
	root, err := cleanRemote(p.Root)
	if err != nil {
		return err
	}
	p.Root = root
	spec, err := m.st.get(p.Conn)
	if err != nil {
		return err
	}
	// The legacy FTP adapter does not expose cancellation of a pending login
	// or data read. Do not start an operation whose advertised Stop is unbounded.
	if spec.Kind == KindFTP {
		return rpc.Errorf(rpc.Unavailable, "Project text search is unavailable over FTP. Use SFTP or SMB for this operation.")
	}
	return m.withReadConnection(ctx, p.Conn, func(ctx context.Context, fs FS) error {
		return files.SearchText(ctx, p.TextSearchParams, s, fs.List, fs.Open)
	})
}
