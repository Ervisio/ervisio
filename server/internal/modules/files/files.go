// Package files implements the bridge methods of the Files section (files.*).
// Methods are documented in docs/api/files.md and docs/api/files-transfer.md.
package files

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Register adds the files.* methods to the registry.
func Register(r *rpc.Registry) {
	r.Handle("files.list", rpc.User, hList)
	r.Handle("files.stat", rpc.User, hStat)
	r.Handle("files.mkdir", rpc.User, hMkdir)
	r.Handle("files.create", rpc.User, hCreate)
	r.Handle("files.rename", rpc.User, hRename)
	r.Handle("files.move", rpc.User, hRename)
	r.Stream("files.copy", rpc.User, hCopy)
	r.Handle("files.delete", rpc.User, hDelete)
	r.Handle("files.trashList", rpc.User, hTrashList)
	r.Handle("files.restore", rpc.User, hRestore)
	r.Handle("files.trashDelete", rpc.User, hTrashDelete)
	r.Handle("files.empty", rpc.User, hTrashEmpty)
	r.Handle("files.trashEmpty", rpc.User, hTrashEmpty)
	r.Handle("files.chmod", rpc.User, hChmod)
	r.Handle("files.chown", rpc.Admin, hChown)
	r.Handle("files.readText", rpc.User, hReadText)
	r.Handle("files.writeText", rpc.User, hWriteText)
	r.Stream("files.search", rpc.User, hSearch)
	r.Handle("files.places", rpc.User, hPlaces)
	r.Handle("files.thumbnail", rpc.User, hThumbnail)
	r.Stream("files.readStream", rpc.User, hReadStream)
	r.Stream("files.writeStream", rpc.User, hWriteStream)
}

// cleanPath requires an absolute path without NUL bytes and returns it cleaned.
func cleanPath(p string) (string, error) {
	if p == "" {
		return "", rpc.Errorf(rpc.Invalid, "A path is missing.")
	}
	if strings.ContainsRune(p, 0) {
		return "", rpc.Errorf(rpc.Invalid, "The path contains an invalid character.")
	}
	if !filepath.IsAbs(p) {
		return "", rpc.Errorf(rpc.Invalid, "The path %q must start with /.", p)
	}
	return filepath.Clean(p), nil
}

func cleanPaths(ps []string) ([]string, error) {
	if len(ps) == 0 {
		return nil, rpc.Errorf(rpc.Invalid, "Nothing was selected.")
	}
	if len(ps) > 10000 {
		return nil, rpc.Errorf(rpc.Invalid, "Too many items at once (limit 10000).")
	}
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		c, err := cleanPath(p)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// validName checks a single file name component.
func validName(n string) error {
	if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\x00") {
		return rpc.Errorf(rpc.Invalid, "%q is not a valid file name.", n)
	}
	return nil
}

// protected paths that may never be deleted or moved away.
var protected = map[string]bool{
	"/": true, "/bin": true, "/sbin": true, "/boot": true, "/dev": true, "/etc": true, "/lib": true,
	"/lib64": true, "/proc": true, "/sys": true, "/usr": true, "/var": true, "/run": true, "/root": true,
	"/home": true, "/opt": true, "/srv": true, "/mnt": true,
}

func checkNotProtected(p string) error {
	if protected[p] {
		return rpc.Errorf(rpc.Forbidden, "%s is a system folder and cannot be removed or moved from here.", p)
	}
	return nil
}

func ctxErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return rpc.Errorf(rpc.Unavailable, "The operation was cancelled.")
	}
	return nil
}

func human(n int64) string {
	const u = "KMGT"
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	f := float64(n)
	i := -1
	for f >= 1024 && i < len(u)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %ciB", f, u[i])
}
