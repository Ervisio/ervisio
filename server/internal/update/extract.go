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
	"slices"
	"strings"
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
	x := &extraction{dst: dst, prefix: strings.TrimSuffix(prefix, "/") + "/", lim: lim, seen: map[string]bool{}}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("archive: %v", err)
		}
		if err := x.entry(h, tr); err != nil {
			return err
		}
	}
}

// extraction holds the running counters of one ExtractTarGz call.
type extraction struct {
	dst, prefix string
	lim         Limits
	seen        map[string]bool
	total       int64
	entries     int
}

func (x *extraction) entry(h *tar.Header, r io.Reader) error {
	if h.Typeflag == tar.TypeXGlobalHeader {
		return nil
	}
	x.entries++
	if x.entries > x.lim.MaxEntries {
		return fmt.Errorf("archive has more than %d entries", x.lim.MaxEntries)
	}
	name := strings.TrimPrefix(h.Name, "./")
	if strings.TrimSuffix(name, "/")+"/" == x.prefix && h.Typeflag == tar.TypeDir {
		return nil // the top-level folder itself
	}
	rel, err := x.relName(h, name)
	if err != nil {
		return err
	}
	switch h.Typeflag {
	case tar.TypeDir:
		return mkdirNoFollow(x.dst, rel)
	case tar.TypeReg:
		return x.file(h, rel, r)
	}
	return fmt.Errorf("archive entry %q is not a regular file or folder (type %q): refused", h.Name, string(h.Typeflag))
}

// relName validates name against the prefix and the seen set and returns the
// stripped relative path.
func (x *extraction) relName(h *tar.Header, name string) (string, error) {
	rel, ok := strings.CutPrefix(name, x.prefix)
	if !ok {
		return "", fmt.Errorf("archive entry %q is outside %s", h.Name, x.prefix)
	}
	rel = strings.TrimSuffix(rel, "/")
	if err := checkRelName(rel); err != nil {
		return "", fmt.Errorf("archive entry %q: %v", h.Name, err)
	}
	if x.seen[rel] {
		return "", fmt.Errorf("archive entry %q appears twice", h.Name)
	}
	x.seen[rel] = true
	return rel, nil
}

func (x *extraction) file(h *tar.Header, rel string, r io.Reader) error {
	if h.Size < 0 || h.Size > x.lim.MaxFile {
		return fmt.Errorf("archive entry %q is larger than %d bytes", h.Name, x.lim.MaxFile)
	}
	x.total += h.Size
	if x.total > x.lim.MaxTotal {
		return fmt.Errorf("archive expands to more than %d bytes", x.lim.MaxTotal)
	}
	if dir := path.Dir(rel); dir != "." {
		if err := mkdirNoFollow(x.dst, dir); err != nil {
			return err
		}
	}
	mode := os.FileMode(0o644)
	if h.Mode&0o111 != 0 {
		mode = 0o755
	}
	if err := writeFileExcl(filepath.Join(x.dst, filepath.FromSlash(rel)), r, h.Size, mode); err != nil {
		return fmt.Errorf("extract %s: %v", rel, err)
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
	if slices.ContainsFunc(strings.Split(rel, "/"), badComponent) {
		return errors.New("name has an empty, '.' or '..' component")
	}
	if strings.ContainsFunc(rel, badRune) {
		return errors.New("name has control characters or backslashes")
	}
	return nil
}

func badComponent(part string) bool { return part == "" || part == "." || part == ".." }

func badRune(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }

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
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|oNoFollow, mode)
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
