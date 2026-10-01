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

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
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
	if !assetIDRe.MatchString(p.ID) || p.File == "" || !fs.ValidPath(p.File) || strings.Contains(p.File, "\\") {
		return rpc.Errorf(rpc.NotFound, "not found")
	}
	if !devEnabled(readPolicy()) {
		return rpc.Errorf(rpc.NotFound, "not found")
	}
	dir := devFolderFor(p.ID)
	if dir == "" {
		return rpc.Errorf(rpc.NotFound, "not found")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return rpc.Errorf(rpc.NotFound, "not found")
	}
	defer root.Close()
	f, err := root.Open(p.File)
	if err != nil {
		return rpc.Errorf(rpc.NotFound, "not found")
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > MaxDevAsset {
		return rpc.Errorf(rpc.NotFound, "not found")
	}
	if err := s.Send(DevAssetMeta{Name: fi.Name(), Size: fi.Size(), Mime: mime.TypeByExtension(filepath.Ext(fi.Name()))}); err != nil {
		return err
	}
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
