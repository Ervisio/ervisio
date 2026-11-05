//go:build unix

package files

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

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
	perm, err := uploadPerm(dfd(d), name, p.Overwrite)
	if err != nil {
		return err
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
	size, complete, err := receiveUpload(ctx, s, tmp, p.Size)
	if err != nil || !complete {
		return err
	}
	if err := unix.Fchmod(int(tmp.Fd()), perm); err != nil {
		return pathErr("chmod", path, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := renameUpload(dfd(d), tname, name, path, p.Overwrite); err != nil {
		return err
	}
	ok = true
	return s.Send(map[string]any{"done": true, "size": size, "path": path})
}

// uploadPerm checks an existing target and returns the mode the upload gets:
// a replaced regular file keeps its permissions.
func uploadPerm(dirfd int, name string, overwrite bool) (uint32, error) {
	perm := uint32(0o644)
	st, err := lstatAt(dirfd, name)
	if err != nil {
		return perm, nil
	}
	if !overwrite {
		return 0, rpc.Errorf(rpc.Conflict, "%s already exists.", name)
	}
	if isDir(&st) {
		return 0, rpc.Errorf(rpc.Invalid, "%s is a folder and cannot be replaced by a file.", name)
	}
	if isReg(&st) {
		perm = st.Mode & 0o777
	}
	return perm, nil
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

// renameUpload moves the finished temp file onto the target.
func renameUpload(dirfd int, tname, name, path string, overwrite bool) error {
	if overwrite {
		if err := unix.Renameat(dirfd, tname, dirfd, name); err != nil {
			return pathErr("rename", path, err)
		}
		return nil
	}
	// Refuse to replace a file that appeared meanwhile.
	err := renameNoReplace(dirfd, tname, dirfd, name, path)
	if rpc.IsCode(err, rpc.Conflict) {
		return rpc.Errorf(rpc.Conflict, "%s already exists.", name)
	}
	return err
}
