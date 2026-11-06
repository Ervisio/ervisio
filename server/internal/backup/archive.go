package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type budget struct {
	limit, used int64
	dir         string
	checked     int64
}
type budgetWriter struct {
	io.Writer
	b *budget
}

func (w budgetWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.b.limit-w.b.used {
		return 0, errors.New("The backup reached its spool size or free-space limit.")
	}
	if w.b.used-w.b.checked >= 8<<20 {
		free, err := availableBytes(w.b.dir)
		if err != nil || free < reserveBytes+int64(len(p)) {
			return 0, errors.New("The spool is running out of free space; no existing backup was removed.")
		}
		w.b.checked = w.b.used
	}
	n, err := w.Writer.Write(p)
	w.b.used += int64(n)
	return n, err
}

type archiveInfo struct {
	Schema       string `json:"schema"`
	JobID        string `json:"jobId"`
	RunID        string `json:"runId"`
	Created      string `json:"created"`
	Mode         string `json:"mode"`
	Files        int    `json:"files"`
	Databases    int    `json:"databases"`
	SourceBytes  int64  `json:"sourceBytes"`
	SkippedLinks int    `json:"skippedLinks"`
	Excluded     int    `json:"excluded"`
}

func excluded(patterns []string, rel string) bool {
	for _, pattern := range patterns {
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				return true
			}
		}
		if match, _ := path.Match(pattern, rel); match {
			return true
		}
		if !strings.Contains(pattern, "/") {
			if match, _ := path.Match(pattern, path.Base(rel)); match {
				return true
			}
		}
	}
	return false
}

type packer struct {
	ctx       context.Context
	tw        *tar.Writer
	info      *archiveInfo
	limit     int64
	ownDir    string
	outputDir string
	entries   int
}

func (p *packer) entry(root *os.Root, rel, target string, fi os.FileInfo) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	p.entries++
	if p.entries >= maxEntries {
		return errors.New("The backup contains too many filesystem entries.")
	}
	if !text(target, 4096) || strings.HasPrefix(target, "/") || strings.Contains(target, `\`) {
		return errors.New("A source file has a name that cannot be archived safely.")
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		p.info.SkippedLinks++
		return nil
	}
	if !fi.IsDir() && !fi.Mode().IsRegular() {
		p.info.Excluded++
		return nil
	}
	h, err := tar.FileInfoHeader(fi, "")
	if err != nil {
		return err
	}
	h.Name = target
	h.Uid = 0
	h.Gid = 0
	h.Uname = ""
	h.Gname = ""
	if fi.IsDir() {
		h.Name += "/"
		return p.tw.WriteHeader(h)
	}
	if fi.Size() < 0 || fi.Size() > p.limit-p.info.SourceBytes {
		return errors.New("The source data exceeds this backup's size limit.")
	}
	f, err := openArchiveRead(root, rel, false)
	if err != nil {
		return errors.New("A source file cannot be read; verify this account's access and exclusions.")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(fi, opened) {
		return errors.New("A source file changed while opening it; try again.")
	}
	if err = p.tw.WriteHeader(h); err != nil {
		return err
	}
	if _, err = copyBounded(p.ctx, p.tw, f, fi.Size()); err != nil {
		return err
	}
	after, err := f.Stat()
	if err != nil || after.Size() != fi.Size() || !after.ModTime().Equal(fi.ModTime()) {
		return errors.New("A source file changed during backup; exclude changing logs or pause its writer and retry.")
	}
	p.info.Files++
	p.info.SourceBytes += fi.Size()
	return nil
}

func (p *packer) folder(source, target string, patterns []string) error {
	fi, err := os.Lstat(source)
	if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("A source folder is missing, is a link or is not readable by this account.")
	}
	resolved, resolveErr := filepath.EvalSymlinks(source)
	if resolveErr != nil {
		return errors.New("The source directory cannot be resolved safely.")
	}
	source = resolved
	if p.outputDir != "" && (source == p.outputDir || strings.HasPrefix(source, p.outputDir+string(filepath.Separator))) {
		return errors.New("A source folder is inside this plan's backup output folder.")
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return errors.New("A source folder cannot be opened by this account.")
	}
	defer root.Close()
	if err = verifySourceRoot(root, fi); err != nil {
		return err
	}
	if err = p.entry(root, ".", target, fi); err != nil {
		return err
	}
	var walk func(string, string, int) error
	walk = func(rel, dest string, depth int) error {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		if depth > 128 {
			return errors.New("A source folder is nested too deeply.")
		}
		dir, err := openArchiveRead(root, rel, true)
		if err != nil {
			return errors.New("A source directory cannot be read.")
		}
		entries, err := dir.ReadDir(maxEntries + 1)
		_ = dir.Close()
		if err != nil && err != io.EOF {
			return errors.New("A source directory cannot be listed.")
		}
		if len(entries) > maxEntries {
			return errors.New("A source directory has too many entries.")
		}
		for _, e := range entries {
			r := path.Join(rel, e.Name())
			nativeRel := filepath.FromSlash(r)
			absolute := filepath.Join(source, nativeRel)
			if absolute == p.ownDir || strings.HasPrefix(absolute, p.ownDir+string(filepath.Separator)) || p.outputDir != "" && (absolute == p.outputDir || strings.HasPrefix(absolute, p.outputDir+string(filepath.Separator))) {
				p.info.Excluded++
				continue
			}
			if excluded(patterns, r) {
				p.info.Excluded++
				continue
			}
			fi, err := root.Lstat(nativeRel)
			if err != nil {
				return errors.New("A source entry disappeared during backup.")
			}
			if err = p.entry(root, nativeRel, path.Join(dest, e.Name()), fi); err != nil {
				return err
			}
			if fi.IsDir() {
				if err = walk(r, path.Join(dest, e.Name()), depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(".", target, 0)
}

func verifySourceRoot(root *os.Root, expected os.FileInfo) error {
	f, err := openArchiveRead(root, ".", true)
	if err != nil {
		return errors.New("The source root changed while opening it.")
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !actual.IsDir() || !os.SameFile(expected, actual) {
		return errors.New("The source root changed while opening it; no backup was published.")
	}
	return nil
}

func copyBounded(ctx context.Context, w io.Writer, r io.Reader, size int64) (int64, error) {
	buffer := make([]byte, 64<<10)
	var done int64
	for done < size {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		want := int64(len(buffer))
		if want > size-done {
			want = size - done
		}
		n, err := r.Read(buffer[:want])
		if n > 0 {
			written, e := w.Write(buffer[:n])
			done += int64(written)
			if e != nil {
				return done, e
			}
			if written != n {
				return done, io.ErrShortWrite
			}
		}
		if err != nil {
			if err == io.EOF && done == size {
				return done, nil
			}
			return done, errors.New("The source data ended early.")
		}
		if n == 0 {
			return done, io.ErrNoProgress
		}
	}
	return done, nil
}

func buildArchive(ctx context.Context, c *Config, runID, spool, ownDir string, b *budget) (string, string, archiveInfo, error) {
	if resolved, err := filepath.EvalSymlinks(ownDir); err == nil {
		ownDir = resolved
	}
	info := archiveInfo{Schema: "ervisio-backup-v1", JobID: c.ID, RunID: runID, Created: time.Now().UTC().Format(time.RFC3339), Mode: c.Mode}
	dumps := []string{}
	if c.Mode != "folders" {
		for i, db := range c.Databases {
			p := filepath.Join(spool, fmt.Sprintf("database-%02d", i+1))
			if err := dumpDatabase(ctx, db, p, b); err != nil {
				return "", "", info, err
			}
			dumps = append(dumps, p)
		}
	}
	output := filepath.Join(spool, "backup.tar.gz")
	f, err := openProtected(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", "", info, err
	}
	hash := sha256.New()
	gz, err := gzip.NewWriterLevel(io.MultiWriter(budgetWriter{f, b}, hash), gzip.BestSpeed)
	if err != nil {
		f.Close()
		return "", "", info, err
	}
	tw := tar.NewWriter(gz)
	p := packer{ctx: ctx, tw: tw, info: &info, limit: c.MaxBytes, ownDir: ownDir}
	if c.Destination.kind() == "local" {
		p.outputDir = filepath.Join(c.Destination.Path, "ervisio-backup-"+c.ID)
	}
	if c.Mode != "databases" {
		for i, source := range c.Folders {
			name := fmt.Sprintf("folders/%02d-%s", i+1, filepath.Base(source))
			if err = p.folder(source, name, c.Excludes); err != nil {
				break
			}
		}
	}
	for i, dump := range dumps {
		if err != nil {
			break
		}
		root, e := os.OpenRoot(spool)
		if e != nil {
			err = e
			break
		}
		fi, e := root.Lstat(filepath.Base(dump))
		if e == nil {
			ext := "sql"
			if c.Databases[i].Kind == "postgres" {
				ext = "dump"
			}
			if c.Databases[i].Kind == "sqlite" {
				ext = "sqlite"
			}
			e = p.entry(root, filepath.Base(dump), fmt.Sprintf("databases/%02d-%s.%s", i+1, c.Databases[i].Name, ext), fi)
		}
		root.Close()
		if e != nil {
			err = e
			break
		}
		info.Databases++
	}
	if err == nil {
		metadata, _ := json.MarshalIndent(info, "", "  ")
		err = tw.WriteHeader(&tar.Header{Name: "ERVISIO-BACKUP.json", Mode: 0600, Size: int64(len(metadata)), ModTime: time.Now()})
		if err == nil {
			_, err = tw.Write(metadata)
		}
	}
	if e := tw.Close(); err == nil {
		err = e
	}
	if e := gz.Close(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return "", "", info, err
	}
	return output, hex.EncodeToString(hash.Sum(nil)), info, nil
}
