package remote

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/pkg/sftp"
)

// Capacity contains server-reported filesystem space, never an estimated share quota.
type Capacity struct {
	Supported bool   `json:"supported"`
	Path      string `json:"path"`
	Total     uint64 `json:"total,omitempty"`
	Used      uint64 `json:"used,omitempty"`
	Free      uint64 `json:"free,omitempty"`
	Avail     uint64 `json:"avail,omitempty"`
}

type capacityFS interface {
	Capacity(context.Context, string) (Capacity, error)
}

func capacityFromBlocks(p string, block, total, free, available uint64) Capacity {
	unknown := Capacity{Path: p}
	if block == 0 || total == 0 || total > math.MaxUint64/block || free > total || available > free {
		return unknown
	}
	return Capacity{Supported: true, Path: p, Total: total * block, Used: (total - free) * block, Free: free * block, Avail: available * block}
}

func (f *sftpFS) Capacity(ctx context.Context, p string) (Capacity, error) {
	if err := ctx.Err(); err != nil {
		return Capacity{}, err
	}
	stat, err := f.c.StatVFS(p)
	if err != nil {
		var status *sftp.StatusError
		if errors.As(err, &status) && (status.Code == 8 || strings.Contains(strings.ToLower(status.Error()), "operation not supported")) {
			return Capacity{Path: p}, nil
		} // SSH_FX_OP_UNSUPPORTED
		return Capacity{}, err
	}
	return capacityFromBlocks(p, stat.Frsize, stat.Blocks, stat.Bfree, stat.Bavail), nil
}

func (f *smbFS) Capacity(ctx context.Context, p string) (Capacity, error) {
	stat, err := f.sh(ctx).Statfs(smbPath(p))
	if err != nil {
		return Capacity{}, err
	}
	// go-smb2 reports sectors per allocation unit, rather than bytes, as FragmentSize.
	sector, sectors := stat.BlockSize(), stat.FragmentSize()
	if sector == 0 || sectors > math.MaxUint64/sector {
		return Capacity{Path: p}, nil
	}
	return capacityFromBlocks(p, sector*sectors, stat.TotalBlockCount(), stat.FreeBlockCount(), stat.AvailableBlockCount()), nil
}

func (m *module) hStatfs(ctx context.Context, c *rpc.Call) (any, error) {
	p, pth, err := bindPathParams(c)
	if err != nil {
		return nil, err
	}
	result := Capacity{Path: pth}
	spec, err := m.st.get(p.Conn)
	if err != nil {
		return nil, err
	}
	if spec.Kind != KindSFTP && spec.Kind != KindSMB {
		return result, nil
	}
	// Overview probes must not fill the session's 24 persistent connection slots.
	err = m.withReadConnection(ctx, p.Conn, func(ctx context.Context, f FS) error {
		if fs, ok := f.(capacityFS); ok {
			result, err = fs.Capacity(ctx, pth)
			return err
		}
		return nil
	})
	return result, err
}
