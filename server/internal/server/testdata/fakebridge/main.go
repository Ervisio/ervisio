// Command fakebridge is a test bridge that adds minimal reference
// implementations of files.readStream / files.writeStream (wire shape in
// docs/api/files-transfer.md) to the normal modules. It is used by the
// daemon's tests only; the real methods live in internal/modules/files.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/Fonlogen/LinuxAdmin/server/internal/modules"
	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

func main() {
	reg := rpc.NewRegistry()
	modules.RegisterAll(reg)
	reg.Stream("files.readStream", rpc.User, readStream)
	reg.Stream("files.writeStream", rpc.User, writeStream)
	_ = rpc.Serve(context.Background(), reg, os.Stdin, os.Stdout, rpc.ServeOptions{UID: os.Geteuid(), Version: "test"})
}

func readStream(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p struct{ Path string }
	if err := c.Bind(&p); err != nil {
		return err
	}
	f, err := os.Open(p.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return rpc.Errorf(rpc.Invalid, "not a regular file")
	}
	if err := s.Send(map[string]any{"name": fi.Name(), "size": fi.Size(), "mime": ""}); err != nil {
		return err
	}
	buf := make([]byte, 64<<10)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if err := s.SendBytes(buf[:n]); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	return s.Send(map[string]bool{"done": true})
}

func writeStream(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p struct {
		Path      string
		Overwrite bool
	}
	if err := c.Bind(&p); err != nil {
		return err
	}
	if _, err := os.Lstat(p.Path); err == nil && !p.Overwrite {
		return rpc.Errorf(rpc.Conflict, "%s already exists", p.Path)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p.Path), ".upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	var size int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case in, ok := <-s.Input():
			if !ok {
				return ctx.Err()
			}
			var msg struct {
				Data string `json:"data"`
				EOF  bool   `json:"eof"`
			}
			if err := json.Unmarshal(in, &msg); err != nil {
				return rpc.Errorf(rpc.Invalid, "bad input")
			}
			if msg.EOF {
				if err := tmp.Close(); err != nil {
					return err
				}
				if err := os.Rename(tmp.Name(), p.Path); err != nil {
					return err
				}
				return s.Send(map[string]any{"done": true, "size": size, "path": p.Path})
			}
			b, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				return rpc.Errorf(rpc.Invalid, "bad base64")
			}
			n, err := tmp.Write(b)
			size += int64(n)
			if err != nil {
				return err
			}
		}
	}
}
