//go:build windows

package files

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	if fi, err := os.Lstat(path); err == nil {
		if !p.Overwrite {
			return rpc.Errorf(rpc.Conflict, "%s already exists.", name)
		}
		if fi.IsDir() && !isLinkMode(fi.Mode()) {
			return rpc.Errorf(rpc.Invalid, "%s is a folder and cannot be replaced by a file.", name)
		}
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
	var size int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case in, open := <-s.Input():
			if !open {
				return ctx.Err()
			}
			var msg struct {
				Data string `json:"data"`
				EOF  bool   `json:"eof"`
			}
			if err := json.Unmarshal(in, &msg); err != nil {
				return rpc.Errorf(rpc.Invalid, "Malformed upload data.")
			}
			if msg.EOF {
				if p.Size >= 0 && size != p.Size {
					return rpc.Errorf(rpc.Invalid, "The upload ended early (%d of %d bytes). Try again.", size, p.Size)
				}
				if err := tmp.Sync(); err != nil {
					return pathErr("write", path, err)
				}
				if err := tmp.Close(); err != nil {
					return err
				}
				var err error
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
			b, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				return rpc.Errorf(rpc.Invalid, "Malformed upload data.")
			}
			n, err := tmp.Write(b)
			size += int64(n)
			if err != nil {
				return err
			}
		}
	}
}
