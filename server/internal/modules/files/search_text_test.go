package files

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type textSearchRecorder struct {
	messages []textSearchMessage
	onSend   func()
}
type textSearchMessage struct {
	Matches   []TextMatch `json:"matches"`
	Done      bool        `json:"done"`
	Truncated bool        `json:"truncated"`
	Count     int         `json:"count"`
	Skipped   int         `json:"skipped"`
}

func (s *textSearchRecorder) Send(value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var m textSearchMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	s.messages = append(s.messages, m)
	if s.onSend != nil {
		s.onSend()
	}
	return nil
}
func (s *textSearchRecorder) SendBytes([]byte) error        { return errors.New("unexpected binary data") }
func (s *textSearchRecorder) Input() <-chan json.RawMessage { return nil }
func (s *textSearchRecorder) matches() (out []TextMatch) {
	for _, m := range s.messages {
		out = append(out, m.Matches...)
	}
	return
}
func projectFile(t *testing.T, root, name, text string) string {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTextSearchRealProjectMatchesLinesAndSkipsGeneratedBinaryAndLinks(t *testing.T) {
	root := t.TempDir()
	app := projectFile(t, root, "src/app.js", "const port = 8080;\n// 🚀 Café NEEDLE here\n")
	config := projectFile(t, root, ".env.example", "needle=value\n")
	projectFile(t, root, "node_modules/dep.js", "needle\n")
	projectFile(t, root, "image.dat", "binary\x00needle\n")
	projectFile(t, root, "large.txt", strings.Repeat("x", textSearchFileBytes+1)+"needle")
	outside := projectFile(t, t.TempDir(), "outside.txt", "needle\n")
	_ = os.Symlink(outside, filepath.Join(root, "external-link.txt"))
	s := &textSearchRecorder{}
	if err := doSearchText(context.Background(), TextSearchParams{Root: root, Query: "needle"}, s); err != nil {
		t.Fatal(err)
	}
	got := s.matches()
	if len(got) != 2 {
		t.Fatalf("matches=%+v", got)
	}
	for _, match := range got {
		if match.Path == app && (match.Line != 2 || match.Column != 12 || !strings.Contains(match.Text, "NEEDLE")) {
			t.Errorf("unicode line/UTF16 column mismatch: %+v", match)
		}
		if match.Path != app && match.Path != config {
			t.Errorf("searched excluded or out-of-project file: %+v", match)
		}
	}
	last := s.messages[len(s.messages)-1]
	if !last.Done || last.Count != 2 || last.Skipped < 3 {
		t.Fatalf("completion=%+v", last)
	}
}

func TestTextSearchLiteralCaseAndResultLimit(t *testing.T) {
	root := t.TempDir()
	projectFile(t, root, "config.txt", "a.*\nA.*\na.*\na.*\n")
	s := &textSearchRecorder{}
	if err := doSearchText(context.Background(), TextSearchParams{Root: root, Query: "a.*", CaseSensitive: true, MaxResults: 2}, s); err != nil {
		t.Fatal(err)
	}
	got := s.matches()
	if len(got) != 2 || got[0].Line != 1 || got[1].Line != 3 {
		t.Fatalf("literal/case/limit=%+v", got)
	}
	if last := s.messages[len(s.messages)-1]; !last.Done || !last.Truncated {
		t.Fatalf("completion=%+v", last)
	}
}

func TestTextSearchGeneratedFoldersAreExplicitlyOptIn(t *testing.T) {
	root := t.TempDir()
	projectFile(t, root, "vendor/generated.txt", "find me")
	s := &textSearchRecorder{}
	if err := doSearchText(context.Background(), TextSearchParams{Root: root, Query: "find", IncludeGenerated: true}, s); err != nil {
		t.Fatal(err)
	}
	if len(s.matches()) != 1 {
		t.Fatalf("expected opted-in generated result: %+v", s.messages)
	}
}

func TestTextSearchCancellationStopsTraversalAndLateCompletion(t *testing.T) {
	root := t.TempDir()
	projectFile(t, root, "many.txt", strings.Repeat("needle\n", 200))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &textSearchRecorder{onSend: cancel}
	if err := doSearchText(ctx, TextSearchParams{Root: root, Query: "needle"}, s); err != nil {
		t.Fatal(err)
	}
	if len(s.matches()) != 25 {
		t.Fatalf("did not stop at first flushed batch: %d", len(s.matches()))
	}
	for _, m := range s.messages {
		if m.Done {
			t.Fatal("cancelled search sent late completion")
		}
	}
}

func TestTextSearchRejectsUnboundedQueryBeforeOpeningFilesystem(t *testing.T) {
	for _, query := range []string{"", "\n", "one\ntwo", strings.Repeat("x", 257)} {
		called := false
		err := SearchText(context.Background(), TextSearchParams{Root: "/", Query: query}, &textSearchRecorder{}, func(context.Context, string) ([]Entry, error) { called = true; return nil, nil }, func(context.Context, string) (io.ReadCloser, error) { called = true; return nil, nil })
		if err == nil || called {
			t.Errorf("query %q validation=%v touched=%v", query, err, called)
		}
	}
}

func TestTextSearchRemoteStyleNamesNeverEscapeProject(t *testing.T) {
	var opened []string
	s := &textSearchRecorder{}
	err := SearchText(context.Background(), TextSearchParams{Root: "/srv/project", Query: "needle"}, s,
		func(context.Context, string) ([]Entry, error) {
			return []Entry{{Name: "../outside", Path: "/outside", Type: "file"}, {Name: "app.js", Path: "/outside", Type: "file"}}, nil
		},
		func(_ context.Context, p string) (io.ReadCloser, error) {
			opened = append(opened, p)
			return io.NopCloser(strings.NewReader("needle")), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0] != "/srv/project/app.js" {
		t.Fatalf("trusted server entry path or escaped project: %+v", opened)
	}
}

type countedTextReader struct {
	io.Reader
	bytes int
}

func (r *countedTextReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.bytes += n
	return n, err
}

func TestTextSearchBoundsReadsEvenWhenFileSizeMetadataIsWrong(t *testing.T) {
	reader := &countedTextReader{Reader: strings.NewReader(strings.Repeat("x", textSearchFileBytes*2))}
	s := &textSearchRecorder{}
	err := SearchText(context.Background(), TextSearchParams{Root: "/project", Query: "needle"}, s,
		func(context.Context, string) ([]Entry, error) {
			return []Entry{{Name: "changed.txt", Type: "file", Size: 0}}, nil
		},
		func(context.Context, string) (io.ReadCloser, error) { return io.NopCloser(reader), nil })
	if err != nil {
		t.Fatal(err)
	}
	if reader.bytes != textSearchFileBytes+1 {
		t.Fatalf("unbounded read: %d", reader.bytes)
	}
	if len(s.matches()) != 0 || s.messages[len(s.messages)-1].Skipped != 1 {
		t.Fatalf("oversized file was searched: %+v", s.messages)
	}
}
