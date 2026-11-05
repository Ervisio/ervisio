package plugins

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// MaxDevAsset bounds one file served from a folder loaded with plugins.loadDev.
const MaxDevAsset = 16 << 20

var assetIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// DevAssetMeta is the first event of plugins.devAsset.
type DevAssetMeta struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Mime string `json:"mime"`
}

// devFolderFor returns the folder registered with plugins.loadDev whose
// manifest id is id, or "".
func devFolderFor(id string) string {
	for _, dir := range registeredDev() {
		if m, err := LoadManifest(dir); err == nil && m.ID == id {
			return dir
		}
	}
	return ""
}

// devAsset implements plugins.devAsset {id, file}: it streams one file of a
// plugin folder loaded with plugins.loadDev, as the signed-in user (so the
// daemon never reads a user-chosen folder with its own rights). The file is
// opened through os.Root, so neither "..", absolute paths nor symlinks can
// leave the folder. Events: DevAssetMeta, then base64 chunks.
func devAsset(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p struct {
		ID   string `json:"id"`
		File string `json:"file"`
	}
	if err := c.Bind(&p); err != nil {
		return err
	}
	f, fi, err := openDevAsset(p.ID, p.File)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := s.Send(DevAssetMeta{Name: fi.Name(), Size: fi.Size(), Mime: mime.TypeByExtension(filepath.Ext(fi.Name()))}); err != nil {
		return err
	}
	return streamFile(ctx, f, s)
}

// openDevAsset opens a regular file of the dev folder for id, through os.Root.
// Every failure is reported as not found.
func openDevAsset(id, file string) (*os.File, os.FileInfo, error) {
	notFound := rpc.Errorf(rpc.NotFound, "not found")
	if !assetIDRe.MatchString(id) || file == "" || !fs.ValidPath(file) || strings.Contains(file, "\\") {
		return nil, nil, notFound
	}
	if !devEnabled(readPolicy()) {
		return nil, nil, notFound
	}
	dir := devFolderFor(id)
	if dir == "" {
		return nil, nil, notFound
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, notFound
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(file)
	if err != nil {
		return nil, nil, notFound
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > MaxDevAsset {
		_ = f.Close()
		return nil, nil, notFound
	}
	return f, fi, nil
}

// streamFile sends the file to s in 64 KiB chunks.
func streamFile(ctx context.Context, f *os.File, s rpc.Stream) error {
	buf := make([]byte, 64<<10)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := f.Read(buf)
		if n > 0 {
			if err := s.SendBytes(buf[:n]); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return rpc.Errorf(rpc.Internal, "read failed")
		}
	}
}
