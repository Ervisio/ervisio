package files

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

// uploadReq is the files.writeStream parameter set.
type uploadReq struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Overwrite bool   `json:"overwrite"`
}

// bindUpload decodes the writeStream parameters and cleans the target path.
func bindUpload(c *rpc.Call) (uploadReq, error) {
	var p uploadReq
	if err := c.Bind(&p); err != nil {
		return p, err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return p, err
	}
	p.Path = path
	return p, nil
}

// receiveUpload writes the base64 chunks from s to w until the eof message.
// complete is false when the input closed first.
func receiveUpload(ctx context.Context, s rpc.Stream, w io.Writer, want int64) (size int64, complete bool, err error) {
	for {
		select {
		case <-ctx.Done():
			return size, false, ctx.Err()
		case in, open := <-s.Input():
			if !open {
				return size, false, ctx.Err()
			}
			n, eof, err := applyUploadMsg(in, w, size, want)
			size += n
			if err != nil {
				return size, false, err
			}
			if eof {
				return size, true, nil
			}
		}
	}
}

// applyUploadMsg handles one upload message: it writes a data chunk to w and
// returns the bytes written, or reports eof after checking the total.
func applyUploadMsg(in []byte, w io.Writer, size, want int64) (n int64, eof bool, err error) {
	var msg struct {
		Data string `json:"data"`
		EOF  bool   `json:"eof"`
	}
	if err := json.Unmarshal(in, &msg); err != nil {
		return 0, false, rpc.Errorf(rpc.Invalid, "Malformed upload data.")
	}
	if msg.EOF {
		if want >= 0 && size != want {
			return 0, false, rpc.Errorf(rpc.Invalid, "The upload ended early (%d of %d bytes). Try again.", size, want)
		}
		return 0, true, nil
	}
	b, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		return 0, false, rpc.Errorf(rpc.Invalid, "Malformed upload data.")
	}
	written, err := w.Write(b)
	return int64(written), false, err
}
