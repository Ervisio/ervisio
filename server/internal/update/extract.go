package update

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// Limits applied while extracting a release archive.
type Limits struct {
	MaxEntries int   // files + folders
	MaxFile    int64 // bytes of one file
	MaxTotal   int64 // bytes of all files
}

// DefaultLimits fit an Ervisio release (two ~20 MB binaries, a few MB of
// web assets) with a wide margin.
var DefaultLimits = Limits{MaxEntries: 20000, MaxFile: 256 << 20, MaxTotal: 1 << 30}

// ExtractTarGz extracts the gzip tar at archive into dst (which must not
// exist; it is created 0755). Every entry must live under the single
// top-level folder prefix ("ervisio-1.2.0-linux-amd64/"), which is
// stripped. Only regular files and folders are accepted: symlinks, hard
// links, devices, FIFOs, absolute names, ".." components and duplicates are
// errors. Files get mode 0755 when any execute bit is set, else 0644;
// owners in the archive are ignored. On error dst is removed.
func ExtractTarGz(archive, dst, prefix string, lim Limits) (err error) {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.Mkdir(dst, 0o755); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dst)
		}
	}()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("archive: %v", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	seen := map[string]bool{}
	var total int64
	entries := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("archive: %v", err)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		entries++
		if entries > lim.MaxEntries {
			return fmt.Errorf("archive has more than %d entries", lim.MaxEntries)
		}
		name := strings.TrimPrefix(h.Name, "./")
		if strings.TrimSuffix(name, "/")+"/" == prefix && h.Typeflag == tar.TypeDir {
			continue // the top-level folder itself
		}
		rel, ok := strings.CutPrefix(name, prefix)
		if !ok {
			return fmt.Errorf("archive entry %q is outside %s", h.Name, prefix)
		}
		rel = strings.TrimSuffix(rel, "/")
		if err := checkRelName(rel); err != nil {
			return fmt.Errorf("archive entry %q: %v", h.Name, err)
		}
		if seen[rel] {
			return fmt.Errorf("archive entry %q appears twice", h.Name)
		}
		seen[rel] = true
		target := filepath.Join(dst, filepath.FromSlash(rel))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := mkdirNoFollow(dst, rel); err != nil {
				return err
			}
		case tar.TypeReg:
			if h.Size < 0 || h.Size > lim.MaxFile {
				return fmt.Errorf("archive entry %q is larger than %d bytes", h.Name, lim.MaxFile)
			}
			total += h.Size
			if total > lim.MaxTotal {
				return fmt.Errorf("archive expands to more than %d bytes", lim.MaxTotal)
			}
			if dir := path.Dir(rel); dir != "." {
				if err := mkdirNoFollow(dst, dir); err != nil {
					return err
				}
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				mode = 0o755
			}
			if err := writeFileExcl(target, tr, h.Size, mode); err != nil {
				return fmt.Errorf("extract %s: %v", rel, err)
			}
		default:
			return fmt.Errorf("archive entry %q is not a regular file or folder (type %q): refused", h.Name, string(h.Typeflag))
		}
	}
	return nil
}

// checkRelName accepts clean, relative, slash-separated names made of
// ordinary characters.
func checkRelName(rel string) error {
	if rel == "" || len(rel) > 1024 {
		return errors.New("empty or too long name")
	}
	if strings.HasPrefix(rel, "/") || path.Clean(rel) != rel {
		return errors.New("name is not a clean relative path")
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("name has an empty, '.' or '..' component")
		}
	}
	for _, r := range rel {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return errors.New("name has control characters or backslashes")
		}
	}
	return nil
}

// mkdirNoFollow creates every folder of rel under root, refusing to pass
// through anything that is not a real folder (no symlinks can exist since
// the archive cannot contain them, but check anyway).
func mkdirNoFollow(root, rel string) error {
	cur := root
	for _, part := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		switch {
		case err == nil && fi.IsDir():
			continue
		case err == nil:
			return fmt.Errorf("%s exists and is not a folder", cur)
		case !errors.Is(err, os.ErrNotExist):
			return err
		}
		if err := os.Mkdir(cur, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func writeFileExcl(target string, r io.Reader, size int64, mode os.FileMode) error {
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, size))
	if err == nil && n != size {
		err = io.ErrUnexpectedEOF
	}
	if err == nil {
		err = f.Chmod(mode) // umask-independent
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
