package remote

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// fakeS3 is a path-style, single-bucket object store kept in memory.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]string
}

func (s *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rest, ok := strings.CutPrefix(r.URL.Path, "/box")
	if !ok || !strings.Contains(r.Header.Get("Authorization"), "Credential=AKID/") {
		s.fail(w, http.StatusForbidden, "AccessDenied")
		return
	}
	key := strings.TrimPrefix(rest, "/")
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && key == "":
		s.list(w, q)
	case r.Method == http.MethodPost && q.Has("delete"):
		var in struct {
			Object []struct{ Key string }
		}
		_ = xml.NewDecoder(r.Body).Decode(&in)
		for _, o := range in.Object {
			delete(s.objects, o.Key)
		}
		_, _ = io.WriteString(w, "<DeleteResult></DeleteResult>")
	case r.Method == http.MethodPut && r.Header.Get("X-Amz-Copy-Source") != "":
		src, _ := url.PathUnescape(r.Header.Get("X-Amz-Copy-Source"))
		body, ok := s.objects[strings.TrimPrefix(src, "box/")]
		if !ok {
			s.fail(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		s.objects[key] = body
		_, _ = io.WriteString(w, "<CopyObjectResult><ETag>x</ETag></CopyObjectResult>")
	case r.Method == http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		s.objects[key] = string(b)
	case r.Method == http.MethodDelete:
		delete(s.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		body, ok := s.objects[key]
		if !ok {
			s.fail(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, body)
		}
	}
}

func (s *fakeS3) fail(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, "<Error><Code>%s</Code><Message>%s</Message></Error>", code, code)
}

func (s *fakeS3) list(w http.ResponseWriter, q url.Values) {
	prefix, delim := q.Get("prefix"), q.Get("delimiter")
	keys := make([]string, 0, len(s.objects))
	for k := range s.objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<ListBucketResult><IsTruncated>false</IsTruncated>")
	folders := map[string]bool{}
	for _, k := range keys {
		rest, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		if i := strings.Index(rest, delim); delim != "" && i >= 0 {
			if f := prefix + rest[:i+1]; !folders[f] {
				folders[f] = true
				fmt.Fprintf(&b, "<CommonPrefixes><Prefix>%s</Prefix></CommonPrefixes>", f)
			}
			continue
		}
		fmt.Fprintf(&b, "<Contents><Key>%s</Key><Size>%d</Size><LastModified>2006-01-02T15:04:05Z</LastModified></Contents>", k, len(s.objects[k]))
	}
	b.WriteString("</ListBucketResult>")
	_, _ = io.WriteString(w, b.String())
}

func s3Spec(t *testing.T, endpoint, accessKey string) *Conn {
	t.Helper()
	secret := "secret"
	c, err := build(specParams{Kind: KindS3, URL: endpoint, Bucket: "box", User: accessKey, Password: &secret, PathStyle: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func names(entries []Entry) string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Type + ":" + e.Name
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func TestS3Files(t *testing.T) {
	store := &fakeS3{objects: map[string]string{"a.txt": "alpha", "docs/b.txt": "beta", "docs/sub/c.txt": "gamma"}}
	srv := httptest.NewServer(store)
	defer srv.Close()
	ctx := context.Background()

	var denied *rpc.Error
	if _, err := dialS3(ctx, s3Spec(t, srv.URL, "WRONG")); !errors.As(err, &denied) || denied.Code != rpc.Forbidden {
		t.Fatalf("wrong key: %v", err)
	}
	f, err := dialS3(ctx, s3Spec(t, srv.URL, "AKID"))
	if err != nil {
		t.Fatal(err)
	}

	list := func(dir string) string {
		t.Helper()
		entries, err := f.List(ctx, dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		return names(entries)
	}
	if got := list("/"); got != "dir:docs file:a.txt" {
		t.Errorf("root: %s", got)
	}
	if got := list("/docs"); got != "dir:sub file:b.txt" {
		t.Errorf("docs: %s", got)
	}
	if _, err := f.List(ctx, "/nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing folder: %v", err)
	}
	if e, err := f.Stat(ctx, "/docs/b.txt"); err != nil || e.Type != "file" || e.Size != 4 || e.MTime == 0 {
		t.Errorf("stat file: %+v %v", e, err)
	}
	if e, err := f.Stat(ctx, "/docs"); err != nil || e.Type != "dir" {
		t.Errorf("stat folder: %+v %v", e, err)
	}
	if _, err := f.Stat(ctx, "/nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat missing: %v", err)
	}

	// An upload goes straight to its name: no temporary object is left.
	if _, err := writeFile(ctx, f, posixOps, "/docs/new.txt", strings.NewReader("hello"), 5, false, nil); err != nil {
		t.Fatal(err)
	}
	if got := list("/docs"); got != "dir:sub file:b.txt file:new.txt" {
		t.Errorf("after upload: %s", got)
	}
	r, err := f.Open(ctx, "/docs/new.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r)
	_ = r.Close()
	if string(body) != "hello" {
		t.Errorf("read back %q", body)
	}

	if err := f.Mkdir(ctx, "/empty"); err != nil {
		t.Fatal(err)
	}
	if got := list("/empty"); got != "" {
		t.Errorf("empty folder: %q", got)
	}
	if err := f.Remove(ctx, "/docs", true); err == nil {
		t.Error("removed a folder that is not empty")
	}
	if err := f.Remove(ctx, "/empty", true); err != nil {
		t.Error(err)
	}

	if err := f.Rename(ctx, "/docs", "/papers"); err != nil {
		t.Fatal(err)
	}
	if got := list("/papers"); got != "dir:sub file:b.txt file:new.txt" {
		t.Errorf("after rename: %s", got)
	}
	if err := f.Rename(ctx, "/a.txt", "/papers/a b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := f.RemoveAll(ctx, "/papers/sub"); err != nil {
		t.Fatal(err)
	}
	if got := list("/"); got != "dir:papers" {
		t.Errorf("root at the end: %s", got)
	}
	if got := list("/papers"); got != "file:a b.txt file:b.txt file:new.txt" {
		t.Errorf("papers at the end: %s", got)
	}
}

func TestS3Spec(t *testing.T) {
	secret := "s"
	ok := specParams{Kind: KindS3, Bucket: "my-bucket", User: "AKID", Password: &secret}
	c, err := build(ok, nil)
	if err != nil || c.Region != "us-east-1" || c.Host != "s3.us-east-1.amazonaws.com" || c.Name != "my-bucket" {
		t.Fatalf("%+v %v", c, err)
	}
	bad := map[string]func(p *specParams){
		"bucket":   func(p *specParams) { p.Bucket = "My_Bucket" },
		"region":   func(p *specParams) { p.Region = "eu central" },
		"key":      func(p *specParams) { p.User = "" },
		"secret":   func(p *specParams) { p.Password = nil },
		"endpoint": func(p *specParams) { p.URL = "https://host/bucket" },
		"scheme":   func(p *specParams) { p.URL = "ftp://host" },
	}
	for name, change := range bad {
		p := ok
		change(&p)
		if _, err := build(p, nil); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// Another protocol does not keep the S3 fields.
	p := specParams{Kind: KindSMB, Host: "nas", Share: "data", User: "u", Bucket: "my-bucket", Region: "x"}
	if c, err := build(p, nil); err != nil || c.Bucket != "" || c.Region != "" {
		t.Errorf("%+v %v", c, err)
	}
}
