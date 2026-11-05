//go:build windows

package files

import (
	"context"
	"os"
	"path/filepath"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// hWriteStream implements files.writeStream (docs/api/files-transfer.md): the
// upload goes to a temp file next to the target and is renamed into place at
// the end. Without overwrite the rename fails if the target exists; with it a
// link at the target is replaced, never written through.
func hWriteStream(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	p, err := bindUpload(c)
	if err != nil {
		return err
	}
	path := p.Path
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
