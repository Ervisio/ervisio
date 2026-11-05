package files

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// TextSearchParams searches literal text, with fixed resource limits shared by local and remote projects.
type TextSearchParams struct {
	Root             string `json:"root"`
	Query            string `json:"query"`
	CaseSensitive    bool   `json:"caseSensitive"`
	IncludeGenerated bool   `json:"includeGenerated"`
	MaxResults       int    `json:"maxResults"`
}

// TextMatch columns use UTF-16 offsets, matching browser editor selections.
type TextMatch struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text"`
}

const (
	textSearchFileBytes  = 1 << 20
	textSearchTotalBytes = 32 << 20
	textSearchEntries    = 10000
)

var generatedSearchDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, ".cache": true, ".next": true, "dist": true, "build": true,
}

var hSearchText = streamHandler(doSearchText)

func doSearchText(ctx context.Context, p TextSearchParams, s rpc.Stream) error {
	root, err := cleanPath(p.Root)
	if err != nil {
		return err
	}
	anchor, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer anchor.Close()
	p.Root = root
	list := func(ctx context.Context, dir string) ([]Entry, error) {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, err
		}
		f, err := anchor.Open(rel)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		items, err := f.ReadDir(textSearchEntries + 1)
		if err != nil && err != io.EOF {
			return nil, err
		}
		out := make([]Entry, 0, len(items))
		for _, d := range items {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			fi, err := d.Info()
			if err == nil {
				out = append(out, Entry{Name: d.Name(), Type: typeOf(fi.Mode()), Size: fi.Size()})
			}
		}
		return out, nil
	}
	open := func(ctx context.Context, name string) (io.ReadCloser, error) {
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return nil, err
		}
		return openSearchTextFile(anchor, rel)
	}
	return SearchText(ctx, p, s, list, open)
}

// SearchText emits matching lines incrementally; callbacks preserve the caller's filesystem and permissions.
// It never follows directory symlinks or requests administrator rights on its own.
func SearchText(ctx context.Context, p TextSearchParams, s rpc.Stream,
	list func(context.Context, string) ([]Entry, error),
	open func(context.Context, string) (io.ReadCloser, error),
) error {
	if strings.TrimSpace(p.Query) == "" || len(p.Query) > 256 || strings.ContainsAny(p.Query, "\r\n\x00") {
		return rpc.Errorf(rpc.Invalid, "Enter a single line of text to search for (up to 256 bytes).")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	limit := p.MaxResults
	if limit <= 0 {
		limit = 300
	}
	limit = min(limit, 500)
	query := p.Query
	if !p.CaseSensitive {
		query = strings.ToLower(query)
	}
	matches := make([]TextMatch, 0, 25)
	count, filesRead, skipped, visited, bytesRead := 0, 0, 0, 0, 0
	reason := ""
	lastFlush := time.Now()
	flush := func(done bool) error {
		if len(matches) == 0 && !done && time.Since(lastFlush) < 150*time.Millisecond {
			return nil
		}
		err := s.Send(map[string]any{"matches": matches, "files": filesRead, "skipped": skipped,
			"bytes": bytesRead, "count": count, "done": done, "truncated": reason != "", "reason": reason})
		matches = make([]TextMatch, 0, 25)
		lastFlush = time.Now()
		return err
	}
	type directory struct {
		path  string
		depth int
	}
	queue := []directory{{p.Root, 0}}
	for len(queue) > 0 && reason == "" {
		if ctx.Err() != nil {
			reason = "time"
			break
		}
		dir := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		entries, err := list(ctx, dir.path)
		if err != nil {
			if dir.path == p.Root {
				return err
			}
			skipped++
			continue
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				reason = "time"
				break
			}
			visited++
			if visited > textSearchEntries {
				reason = "files"
				break
			}
			if entry.Name == "" || entry.Name == "." || entry.Name == ".." || strings.ContainsAny(entry.Name, "/\\\x00") {
				skipped++
				continue
			}
			name := filepath.Join(dir.path, entry.Name)
			if strings.HasPrefix(p.Root, "/") {
				name = path.Join(dir.path, entry.Name)
			}
			if entry.Type == "dir" {
				if dir.depth < 40 && (p.IncludeGenerated || !generatedSearchDirs[entry.Name]) && !skipSearch[name] {
					queue = append(queue, directory{name, dir.depth + 1})
				} else {
					skipped++
				}
				continue
			}
			if entry.Type != "file" || entry.Size > textSearchFileBytes {
				skipped++
				continue
			}
			if bytesRead >= textSearchTotalBytes {
				reason = "bytes"
				break
			}
			reader, err := open(ctx, name)
			if err != nil {
				skipped++
				continue
			}
			buf, readErr := io.ReadAll(io.LimitReader(reader, int64(min(textSearchFileBytes+1, textSearchTotalBytes-bytesRead+1))))
			_ = reader.Close()
			bytesRead += len(buf)
			if readErr != nil || len(buf) > textSearchFileBytes {
				skipped++
				continue
			}
			if bytesRead > textSearchTotalBytes {
				reason = "bytes"
				break
			}
			text, _, err := decodeText(buf, false)
			if err != nil {
				skipped++
				continue
			}
			filesRead++
			for number, line := range strings.Split(text, "\n") {
				if ctx.Err() != nil {
					reason = "time"
					break
				}
				look := line
				if !p.CaseSensitive {
					look = strings.ToLower(line)
				}
				at := strings.Index(look, query)
				if at < 0 {
					continue
				}
				// Unicode lowercase can change byte lengths; locate the same character position in the original.
				prefix := look[:at]
				prefixRunes := len([]rune(prefix))
				runes := []rune(strings.TrimSuffix(line, "\r"))
				prefixRunes = min(prefixRunes, len(runes))
				column := len(utf16.Encode(runes[:prefixRunes])) + 1
				start, end := max(0, prefixRunes-60), min(len(runes), prefixRunes+180)
				snippet := string(runes[start:end])
				if start > 0 {
					snippet = "…" + snippet
				}
				if end < len(runes) {
					snippet += "…"
				}
				matches = append(matches, TextMatch{Path: name, Line: number + 1, Column: column, Text: snippet})
				count++
				if count >= limit {
					reason = "results"
					break
				}
				if len(matches) >= 25 {
					if err := flush(false); err != nil {
						return err
					}
				}
			}
			if reason != "" {
				break
			}
			if time.Since(lastFlush) >= 150*time.Millisecond {
				if err := flush(false); err != nil {
					return err
				}
			}
		}
	}
	// Closing the stream cancels work without sending a late completion to a replacement search.
	if ctx.Err() == context.Canceled {
		return nil
	}
	return flush(true)
}
