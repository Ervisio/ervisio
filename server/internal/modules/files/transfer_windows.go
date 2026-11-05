//go:build windows

package files

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// hWriteStream implements files.writeStream (docs/api/files-transfer.md): the
// upload goes to a temp file next to the target and is renamed into place at
// the end. Without overwrite the rename fails if the target exists; with it a
// link at the target is replaced, never written through.
func hWriteStream(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p struct {
		Path      string `json:"path"`
		Size      int64  `json:"size"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := c.Bind(&p); err != nil {
		return err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return err
	}
	name := filepath.Base(path)
	if err := validName(name); err != nil || filepath.Dir(path) == path {
		return rpc.Errorf(rpc.Invalid, "That is not a valid target file name.")
	}
	if err := checkUploadTarget(path, name, p.Overwrite); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return pathErr("create", path, err)
	}
	tname := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			_ = os.Remove(tname)
		}
	}()
	size, complete, err := receiveUpload(ctx, s, tmp, p.Size)
	if err != nil || !complete {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return pathErr("write", path, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if p.Overwrite {
		err = replaceFile(tname, path)
	} else {
		err = moveNoReplace(tname, path)
	}
	if err != nil {
		return err
	}
	ok = true
	return s.Send(map[string]any{"done": true, "size": size, "path": path})
}

// checkUploadTarget refuses an existing target unless overwrite is set, and
// never replaces a real folder.
func checkUploadTarget(path, name string, overwrite bool) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil
	}
	if !overwrite {
		return rpc.Errorf(rpc.Conflict, "%s already exists.", name)
	}
	if fi.IsDir() && !isLinkMode(fi.Mode()) {
		return rpc.Errorf(rpc.Invalid, "%s is a folder and cannot be replaced by a file.", name)
	}
	return nil
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
			var msg struct {
				Data string `json:"data"`
				EOF  bool   `json:"eof"`
			}
			if err := json.Unmarshal(in, &msg); err != nil {
				return size, false, rpc.Errorf(rpc.Invalid, "Malformed upload data.")
			}
			if msg.EOF {
				if want >= 0 && size != want {
					return size, false, rpc.Errorf(rpc.Invalid, "The upload ended early (%d of %d bytes). Try again.", size, want)
				}
				return size, true, nil
			}
			b, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				return size, false, rpc.Errorf(rpc.Invalid, "Malformed upload data.")
			}
			n, err := w.Write(b)
			size += int64(n)
			if err != nil {
				return size, false, err
			}
		}
	}
}
