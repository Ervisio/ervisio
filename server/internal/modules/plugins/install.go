package plugins

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Extraction limits.
const (
	maxArchiveBytes = 32 << 20 // compressed download
	maxFileBytes    = 16 << 20 // one extracted file
	maxTotalBytes   = 64 << 20 // all extracted files
	maxEntries      = 2000
)

// ExtractTarGz unpacks a .tar.gz into dest (which must exist and be empty).
// It accepts only directories and regular files; absolute paths, "..",
// backslashes, symlinks, hard links and special files are errors. Files get
// mode 0644 (0755 when the archive marks them executable), directories 0755.
// A single top-level folder shared by every entry is stripped.
func ExtractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("the file is not a .tar.gz archive")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	type entry struct {
		name string
		dir  bool
		exec bool
		data []byte
	}
	// Read everything into memory first (bounded) so the top-level folder can be detected.
	var entries []entry
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("the archive is damaged: %v", err)
		}
		if len(entries) >= maxEntries {
			return fmt.Errorf("the archive holds more than %d entries", maxEntries)
		}
		name := h.Name
		if strings.ContainsAny(name, "\\\x00") {
			return fmt.Errorf("archive entry %q has an invalid name", name)
		}
		name = strings.TrimPrefix(name, "./")
		if name == "" || name == "." {
			continue
		}
		clean := strings.TrimSuffix(name, "/")
		if path.IsAbs(clean) || path.Clean(clean) != clean || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("archive entry %q would leave the plugin folder", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			entries = append(entries, entry{name: clean, dir: true})
		case tar.TypeReg:
			if h.Size > maxFileBytes {
				return fmt.Errorf("archive entry %q is larger than %d MiB", h.Name, maxFileBytes>>20)
			}
			total += h.Size
			if total > maxTotalBytes {
				return fmt.Errorf("the archive expands to more than %d MiB", maxTotalBytes>>20)
			}
			data, err := io.ReadAll(io.LimitReader(tr, h.Size+1))
			if err != nil || int64(len(data)) != h.Size {
				return fmt.Errorf("archive entry %q is damaged", h.Name)
			}
			entries = append(entries, entry{name: clean, exec: h.Mode&0o111 != 0, data: data})
		case tar.TypeXGlobalHeader:
		default:
			return fmt.Errorf("archive entry %q is a link or special file, which plugins may not contain", h.Name)
		}
	}
	if len(entries) == 0 {
		return fmt.Errorf("the archive is empty")
	}
	// Strip a single top-level folder.
	strip := ""
	top := ""
	multi := false
	for _, e := range entries {
		first, rest, hasRest := strings.Cut(e.name, "/")
		if !hasRest && !e.dir {
			multi = true // a file at the top level
			break
		}
		_ = rest
		if top == "" {
			top = first
		} else if top != first {
			multi = true
			break
		}
	}
	if !multi && top != "" {
		strip = top + "/"
	}
	for _, e := range entries {
		name := e.name
		if strip != "" {
			if name == top {
				continue
			}
			name = strings.TrimPrefix(name, strip)
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if rel, err := filepath.Rel(dest, target); err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return fmt.Errorf("archive entry %q would leave the plugin folder", e.name)
		}
		if e.dir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if e.exec {
			mode = 0o755
		}
		// O_EXCL: a duplicate entry (or a file through a planted link) fails instead of overwriting.
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return fmt.Errorf("could not unpack %q: %v", e.name, cleanErr(err))
		}
		if _, err := f.Write(e.data); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

// downloadClient downloads plugin packages (tests replace it).
var downloadClient = httpClient

// installRequest are the plugins.install params.
type installRequest struct {
	Source string `json:"source"`
	SHA256 string `json:"sha256"`
	// Consent is the capabilities object the user was shown. When set, the
	// package must declare exactly that.
	Consent *Capabilities `json:"consent"`
}

func install(ctx context.Context, req installRequest) (*Info, error) {
	p := readPolicy()
	if req.Source == "" {
		return nil, rpc.Errorf(rpc.Invalid, "Say where to install from: an https:// address or the path of a .tar.gz file.")
	}
	if req.SHA256 != "" && !shaRe.MatchString(strings.ToLower(req.SHA256)) {
		return nil, rpc.Errorf(rpc.Invalid, "The checksum must be a 64-character sha256 in hex.")
	}
	if err := os.MkdirAll(InstalledDir, 0o755); err != nil {
		return nil, rpc.Errorf(rpc.Unavailable, "Cannot create %s: %v", InstalledDir, cleanErr(err))
	}
	tmp, err := os.MkdirTemp(InstalledDir, ".install-")
	if err != nil {
		return nil, rpc.Errorf(rpc.Unavailable, "Cannot write to %s: %v", InstalledDir, cleanErr(err))
	}
	defer os.RemoveAll(tmp)

	archive, err := fetchArchive(ctx, req.Source, req.SHA256)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	stage := filepath.Join(tmp, "stage")
	if err := os.Mkdir(stage, 0o755); err != nil {
		return nil, err
	}
	if err := ExtractTarGz(archive, stage); err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "%v", err)
	}
	m, err := LoadManifest(stage)
	if err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "This is not a valid plugin: %v", err)
	}
	sig := CheckSignature(stage, Keys())
	if !sig.Verified {
		if sig.Signed {
			return nil, rpc.Errorf(rpc.Forbidden, "The signature of %s is not valid (%s). It was not installed.", m.Name, sig.Err)
		}
		if !p.AllowUnsigned {
			return nil, rpc.Errorf(rpc.Forbidden, "%s is not signed and this server only accepts signed plugins. Turn on plugins.allow_unsigned in Settings to install it.", m.Name)
		}
	}
	if msg := m.CoreProblem(); msg != "" {
		return nil, rpc.Errorf(rpc.Conflict, "%s Nothing was installed.", msg)
	}
	if req.Consent != nil && !sameJSON(emptyIfNil(*req.Consent), m.Capabilities) {
		return nil, rpc.Errorf(rpc.Conflict, "%s asks for different permissions than the ones you were shown. Nothing was installed; open Browse again to review them.", m.Name)
	}
	if ex := find(p, m.ID); ex != nil && ex.Location != LocInstalled {
		if ex.Location == LocSystem {
			return nil, rpc.Errorf(rpc.Conflict, "%s is part of the system package and cannot be replaced from here.", m.Name)
		}
	}
	final := filepath.Join(InstalledDir, m.ID)
	old := filepath.Join(tmp, "old")
	if _, err := os.Lstat(final); err == nil {
		if err := os.Rename(final, old); err != nil {
			return nil, rpc.Errorf(rpc.Internal, "Cannot replace the installed version: %v", cleanErr(err))
		}
	}
	if err := os.Rename(stage, final); err != nil {
		if _, e := os.Lstat(old); e == nil {
			_ = os.Rename(old, final)
		}
		return nil, rpc.Errorf(rpc.Internal, "Cannot move the plugin into place: %v", cleanErr(err))
	}
	// New installs start enabled.
	st := readState()
	if _, ok := st.Enabled[m.ID]; !ok {
		st.Enabled[m.ID] = true
		_ = writeState(st)
	}
	for _, in := range list(true) {
		if in.ID == m.ID {
			return &in, nil
		}
	}
	return nil, rpc.Errorf(rpc.Internal, "Installed, but the plugin does not show up in the list.")
}

// fetchArchive opens a local .tar.gz or downloads an https one into a temp file.
func fetchArchive(ctx context.Context, source, wantSHA string) (io.ReadCloser, error) {
	var f *os.File
	if u, err := url.Parse(source); err == nil && u.Scheme != "" && u.Host != "" {
		if u.Scheme != "https" {
			return nil, rpc.Errorf(rpc.Invalid, "Only https:// addresses are accepted for downloads.")
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		resp, err := downloadClient().Do(req)
		if err != nil {
			return nil, rpc.Errorf(rpc.Unavailable, "Download failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, rpc.Errorf(rpc.Unavailable, "The download server answered %s.", resp.Status)
		}
		tmp, err := os.CreateTemp(InstalledDir, ".download-*")
		if err != nil {
			return nil, err
		}
		n, err := io.Copy(tmp, io.LimitReader(resp.Body, maxArchiveBytes+1))
		if err != nil || n > maxArchiveBytes {
			tmp.Close()
			os.Remove(tmp.Name())
			return nil, rpc.Errorf(rpc.Invalid, "The download is interrupted or larger than %d MiB.", maxArchiveBytes>>20)
		}
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		f = tmp
	} else {
		if !filepath.IsAbs(source) {
			return nil, rpc.Errorf(rpc.Invalid, "Use an absolute path or an https:// address.")
		}
		fi, err := os.Stat(source)
		if err != nil {
			return nil, rpc.Errorf(rpc.NotFound, "Cannot read %s: %v", source, cleanErr(err))
		}
		if !fi.Mode().IsRegular() || fi.Size() > maxArchiveBytes {
			return nil, rpc.Errorf(rpc.Invalid, "%s must be a .tar.gz file of at most %d MiB.", source, maxArchiveBytes>>20)
		}
		if f, err = os.Open(source); err != nil {
			return nil, rpc.Errorf(rpc.NotFound, "Cannot read %s: %v", source, cleanErr(err))
		}
	}
	if wantSHA != "" {
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return nil, err
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(wantSHA) {
			f.Close()
			return nil, rpc.Errorf(rpc.Invalid, "The checksum does not match (expected %s…, got %s…). Nothing was installed.", wantSHA[:12], got[:12])
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
	}
	return &tempFile{f}, nil
}

// tempFile removes downloaded temp files on Close.
type tempFile struct{ *os.File }

func (t *tempFile) Close() error {
	err := t.File.Close()
	if strings.HasPrefix(filepath.Base(t.Name()), ".download-") {
		os.Remove(t.Name())
	}
	return err
}

func uninstall(id string) error {
	if !idRe.MatchString(id) {
		return rpc.Errorf(rpc.Invalid, "%q is not a plugin id.", id)
	}
	dir := filepath.Join(InstalledDir, id)
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() {
		if f := find(readPolicy(), id); f != nil {
			return rpc.Errorf(rpc.Forbidden, "%s is not installed from Browse (it lives in %s), so it cannot be removed here.", f.M.Name, f.Dir)
		}
		return rpc.Errorf(rpc.NotFound, "There is no installed plugin %q.", id)
	}
	if err := os.RemoveAll(dir); err != nil {
		return rpc.Errorf(rpc.Internal, "Could not remove %s: %v", dir, cleanErr(err))
	}
	st := readState()
	delete(st.Enabled, id)
	_ = writeState(st)
	return nil
}
