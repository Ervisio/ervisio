package files

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

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

// hWriteStream implements files.writeStream (docs/api/files-transfer.md).
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
	if err := validName(filepath.Base(path)); err != nil || path == "/" {
		return rpc.Errorf(rpc.Invalid, "That is not a valid target file name.")
	}
	d, name, err := openParent(path)
	if err != nil {
		return err
	}
	defer d.Close()
	perm := uint32(0o644)
	if st, err := lstatAt(dfd(d), name); err == nil {
		if !p.Overwrite {
			return rpc.Errorf(rpc.Conflict, "%s already exists.", name)
		}
		if isDir(&st) {
			return rpc.Errorf(rpc.Invalid, "%s is a folder and cannot be replaced by a file.", name)
		}
		if isReg(&st) {
			perm = st.Mode & 0o777
		}
	}
	tmp, tname, err := createTempAt(dfd(d), ".upload-", filepath.Dir(path))
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			_ = unix.Unlinkat(dfd(d), tname, 0)
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
				if err := unix.Fchmod(int(tmp.Fd()), perm); err != nil {
					return pathErr("chmod", path, err)
				}
				if err := tmp.Close(); err != nil {
					return err
				}
				if !p.Overwrite {
					// Refuse to replace a file that appeared meanwhile.
					if err := renameNoReplace(dfd(d), tname, dfd(d), name, path); err != nil {
						if rpc.IsCode(err, rpc.Conflict) {
							return rpc.Errorf(rpc.Conflict, "%s already exists.", name)
						}
						return err
					}
				} else if err := unix.Renameat(dfd(d), tname, dfd(d), name); err != nil {
					return pathErr("rename", path, err)
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
