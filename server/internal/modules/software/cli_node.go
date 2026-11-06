package software

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/sys"
)

var nodeVersionRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
var nodeClient = &http.Client{Timeout: 4 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) > 5 || req.URL.Scheme != "https" || req.URL.Host != "nodejs.org" {
		return fmt.Errorf("Node download redirected outside nodejs.org")
	}
	return nil
}}

func nodeFetch(ctx context.Context, url string, writer io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := nodeClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Node download returned HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(writer, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("Node download exceeded its size limit")
	}
	return nil
}

func nodeRelease(index []byte, goos, arch string) (string, string, error) {
	var versions []struct {
		Version string          `json:"version"`
		LTS     json.RawMessage `json:"lts"`
		Files   []string        `json:"files"`
	}
	if err := json.Unmarshal(index, &versions); err != nil {
		return "", "", err
	}
	platform := goos
	if platform == "windows" {
		platform = "win"
	}
	architecture := map[string]string{"amd64": "x64", "arm64": "arm64"}[arch]
	if architecture == "" || (platform != "linux" && platform != "win") {
		return "", "", fmt.Errorf("Managed Node supports Linux/Windows x64 and ARM64")
	}
	filePlatform := platform + "-" + architecture
	if platform == "win" {
		filePlatform += "-zip"
	}
	for _, v := range versions {
		var lts string
		if json.Unmarshal(v.LTS, &lts) != nil || lts == "" || !nodeVersionRE.MatchString(v.Version) {
			continue
		}
		for _, f := range v.Files {
			if f == filePlatform {
				ext := ".tar.gz"
				if platform == "win" {
					ext = ".zip"
				}
				return v.Version, "node-" + v.Version + "-" + platform + "-" + architecture + ext, nil
			}
		}
	}
	return "", "", fmt.Errorf("No supported official Node LTS release found")
}

func nodeChecksum(manifest []byte, name string) (string, error) {
	for _, line := range strings.Split(string(manifest), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && strings.TrimPrefix(parts[1], "*") == name {
			decoded, err := hex.DecodeString(parts[0])
			if err == nil && len(decoded) == 32 {
				return strings.ToLower(parts[0]), nil
			}
		}
	}
	return "", fmt.Errorf("Node archive is absent from the official SHA256 manifest")
}

// Only node and npm are extracted. Archives cannot create links, special files,
// drive paths or parent traversal; both file count and expanded bytes are capped.
func nodeArchivePath(name, top string) (string, bool, error) {
	if strings.Contains(name, "\\") {
		return "", false, fmt.Errorf("Invalid archive path")
	}
	name = strings.TrimSuffix(name, "/")
	if name == top {
		return "", false, nil
	}
	prefix, rel, ok := strings.Cut(name, "/")
	if !ok || prefix != top || !fs.ValidPath(rel) || strings.Contains(rel, ":") {
		return "", false, fmt.Errorf("Invalid archive path")
	}
	allowed := rel == "bin/node" || rel == "node.exe" || rel == "npm.cmd" || rel == "npm.ps1" || rel == "npx.cmd" || rel == "npx.ps1" || rel == "lib" || rel == "lib/node_modules" || rel == "node_modules" || strings.HasPrefix(rel, "lib/node_modules/npm/") || rel == "lib/node_modules/npm" || strings.HasPrefix(rel, "node_modules/npm/") || rel == "node_modules/npm"
	return rel, allowed, nil
}

func unpackNode(archive, dest, top string) error {
	files := 0
	var total int64
	write := func(name string, mode os.FileMode, size int64, r io.Reader) error {
		files++
		if files > 20000 {
			return fmt.Errorf("Node archive contains too many entries")
		}
		if size < 0 || size > 64<<20 {
			return fmt.Errorf("Node archive entry is too large")
		}
		total += size
		if total > 512<<20 {
			return fmt.Errorf("Expanded Node archive is too large")
		}
		rel, allowed, err := nodeArchivePath(name, top)
		if err != nil || !allowed {
			return err
		}
		if mode&os.ModeSymlink != 0 {
			return nil
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if mode.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !mode.IsRegular() || size < 0 || size > 64<<20 {
			return fmt.Errorf("Unsupported Node archive entry")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(f, r, size)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if strings.HasSuffix(archive, ".zip") {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer z.Close()
		for _, f := range z.File {
			r, err := f.Open()
			if err != nil {
				return err
			}
			err = write(f.Name, f.Mode(), int64(f.UncompressedSize64), r)
			_ = r.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, 600<<20))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		mode := h.FileInfo().Mode()
		if h.Typeflag == tar.TypeLink {
			mode = os.ModeSymlink
		}
		if err := write(h.Name, mode, h.Size, tr); err != nil {
			return err
		}
	}
}

func installNode(ctx context.Context, root string, log func(string)) error {
	var index strings.Builder
	if err := nodeFetch(ctx, "https://nodejs.org/dist/index.json", &index, 2<<20); err != nil {
		return err
	}
	version, name, err := nodeRelease([]byte(index.String()), runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	log("Downloading official Node.js LTS " + version + " for this account")
	var manifest strings.Builder
	base := "https://nodejs.org/dist/" + version + "/"
	if err := nodeFetch(ctx, base+"SHASUMS256.txt", &manifest, 1<<20); err != nil {
		return err
	}
	sum, err := nodeChecksum([]byte(manifest.String()), name)
	if err != nil {
		return err
	}
	scratch, err := os.MkdirTemp(root, ".node-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	archive := filepath.Join(scratch, name)
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	err = nodeFetch(ctx, base+name, io.MultiWriter(f, hash), 100<<20)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(hash.Sum(nil)) != sum {
		return fmt.Errorf("Official Node archive checksum does not match")
	}
	log("SHA-256 verified; extracting node and npm")
	stage := filepath.Join(scratch, "runtime")
	if err := os.Mkdir(stage, 0700); err != nil {
		return err
	}
	top := strings.TrimSuffix(strings.TrimSuffix(name, ".tar.gz"), ".zip")
	if err := unpackNode(archive, stage, top); err != nil {
		return err
	}
	node := filepath.Join(stage, "bin", "node")
	if runtime.GOOS == "windows" {
		node = filepath.Join(stage, "node.exe")
	}
	out, err := (sys.Cmd{Name: node, Args: []string{"--version"}, Timeout: 10 * time.Second, MaxOutput: 1024}).Output(ctx)
	if err != nil {
		return fmt.Errorf("Node LTS is not compatible with this host: %w", err)
	}
	if strings.TrimSpace(string(out)) != version {
		return fmt.Errorf("Node version verification failed")
	}
	if err := prepareNodeLaunchers(ctx, stage, filepath.Join(root, "node")); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(stage, ".ervisio-version"), []byte(version+"\n"), 0600); err != nil {
		return err
	}
	return adoptCLI(stage, filepath.Join(root, "node"))
}

func prepareNodeLaunchers(ctx context.Context, stage, target string) error {
	node := filepath.Join(stage, "bin", "node")
	npmBase := filepath.Join(stage, "lib", "node_modules", "npm", "bin")
	if runtime.GOOS == "windows" {
		node = filepath.Join(stage, "node.exe")
		npmBase = filepath.Join(stage, "node_modules", "npm", "bin")
	}
	for _, name := range []string{"npm", "npx"} {
		entry := filepath.Join(npmBase, name+"-cli.js")
		if fi, err := os.Lstat(entry); err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("Official Node archive has no usable %s script", name)
		}
		if runtime.GOOS == "windows" {
			for _, ext := range []string{".cmd"} {
				if fi, err := os.Lstat(filepath.Join(stage, name+ext)); err != nil || !fi.Mode().IsRegular() {
					return fmt.Errorf("Official Node archive has no %s%s launcher", name, ext)
				}
			}
		} else {
			command := cliQuote(filepath.Join(target, "bin", "node")) + " " + cliQuote(filepath.Join(target, "lib", "node_modules", "npm", "bin", name+"-cli.js"))
			if err := writeFileAtomic(filepath.Join(stage, "bin", name), []byte("#!/bin/sh\nexec "+command+" \"$@\"\n"), 0700); err != nil {
				return err
			}
		}
	}
	out, err := (sys.Cmd{Name: node, Args: []string{filepath.Join(npmBase, "npm-cli.js"), "--version"}, Timeout: 15 * time.Second, MaxOutput: 1024}).Output(ctx)
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("Official npm failed its version verification: %v", err)
	}
	return nil
}

func rollbackCLI(target string) error {
	root := filepath.Dir(target)
	quarantine, err := os.MkdirTemp(root, ".failed-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(quarantine)
	if err := checkTrustedDir(target, os.Geteuid()); err != nil {
		return err
	}
	if err := os.Rename(target, filepath.Join(quarantine, "program")); err != nil {
		return err
	}
	previous := target + ".previous"
	if _, err := os.Lstat(previous); os.IsNotExist(err) {
		return nil
	}
	if err := checkTrustedDir(previous, os.Geteuid()); err != nil {
		return err
	}
	return os.Rename(previous, target)
}

func adoptCLI(stage, target string) error {
	previous := target + ".previous"
	if _, err := os.Lstat(previous); err == nil {
		if err := checkTrustedDir(previous, os.Geteuid()); err != nil {
			return err
		}
		if err := os.RemoveAll(previous); err != nil {
			return err
		}
	}
	hadPrevious := false
	if _, err := os.Lstat(target); err == nil {
		if err := checkTrustedDir(target, os.Geteuid()); err != nil {
			return err
		}
		if err := os.Rename(target, previous); err != nil {
			return err
		}
		hadPrevious = true
	}
	if err := os.Rename(stage, target); err != nil {
		if hadPrevious {
			_ = os.Rename(previous, target)
		}
		return err
	}
	return nil
}
