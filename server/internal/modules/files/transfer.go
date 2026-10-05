package files

import (
	"context"
	"io"
	"net/http"
	"os"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// hReadStream implements files.readStream (docs/api/files-transfer.md).
func hReadStream(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p struct {
		Path string `json:"path"`
	}
	if err := c.Bind(&p); err != nil {
		return err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return rpc.Errorf(rpc.Invalid, "Only files can be downloaded. Select a file, not a folder.")
	}
	mt := mimeOf(fi.Name())
	if mt == "" {
		head := make([]byte, 512)
		n, _ := f.Read(head)
		mt = http.DetectContentType(head[:n])
		if _, err := f.Seek(0, 0); err != nil {
			return err
		}
	}
	size := fi.Size()
	if err := s.Send(map[string]any{"name": fi.Name(), "size": size, "mime": mt}); err != nil {
		return err
	}
	buf := make([]byte, 64<<10)
	var sent int64
	for sent < size {
		if err := ctx.Err(); err != nil {
			return err
		}
		want := int64(len(buf))
		if size-sent < want {
			want = size - sent
		}
		n, err := f.Read(buf[:want])
		if n > 0 {
			if err := s.SendBytes(buf[:n]); err != nil {
				return err
			}
			sent += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if sent != size {
		return rpc.Errorf(rpc.Internal, "The file changed while it was being read.")
	}
	return s.Send(map[string]bool{"done": true})
}
