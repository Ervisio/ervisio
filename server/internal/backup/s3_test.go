package backup

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type s3FixtureObject struct {
	data     []byte
	etag     string
	metadata http.Header
}
type s3FixtureUpload struct {
	key      string
	parts    map[int][]byte
	metadata http.Header
}
type s3Fixture struct {
	mu                    sync.Mutex
	objects               map[string]s3FixtureObject
	uploads               map[string]s3FixtureUpload
	ignoreConditions      bool
	failParts             bool
	signed, parts, aborts int
	deleted               []string
}

func s3XML(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(value)
}
func s3FixtureError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte("<Error><Code>" + code + "</Code><Message>Fixture error</Message></Error>"))
}
func (f *s3Fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=fixture-access/") && r.Header.Get("X-Amz-Security-Token") == "fixture-session" {
		f.signed++
	} else {
		s3FixtureError(w, 403, "InvalidAccessKeyId")
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/fixture-bucket/")
	q := r.URL.Query()
	if r.Method == "GET" && q.Get("list-type") == "2" {
		type object struct {
			Key          string
			ETag         string
			Size         int
			LastModified string
		}
		result := struct {
			XMLName     xml.Name `xml:"ListBucketResult"`
			Name        string
			IsTruncated bool
			Contents    []object
		}{Name: "fixture-bucket", Contents: []object{}}
		for name, value := range f.objects {
			if strings.HasPrefix(name, q.Get("prefix")) {
				result.Contents = append(result.Contents, object{name, value.etag, len(value.data), time.Now().UTC().Format(time.RFC3339)})
			}
		}
		s3XML(w, result)
		return
	}
	if r.Method == "POST" && q.Has("uploads") {
		id := newID()
		f.uploads[id] = s3FixtureUpload{key: key, parts: map[int][]byte{}, metadata: r.Header.Clone()}
		s3XML(w, struct {
			XMLName               xml.Name `xml:"InitiateMultipartUploadResult"`
			Bucket, Key, UploadId string
		}{Bucket: "fixture-bucket", Key: key, UploadId: id})
		return
	}
	if id := q.Get("uploadId"); id != "" {
		upload, ok := f.uploads[id]
		if !ok {
			s3FixtureError(w, 404, "NoSuchUpload")
			return
		}
		if r.Method == "DELETE" {
			delete(f.uploads, id)
			f.aborts++
			w.WriteHeader(204)
			return
		}
		if r.Method == "PUT" {
			if f.failParts {
				s3FixtureError(w, 400, "InvalidRequest")
				return
			}
			number, _ := strconv.Atoi(q.Get("partNumber"))
			body, _ := io.ReadAll(r.Body)
			upload.parts[number] = body
			f.parts++
			w.Header().Set("ETag", `"`+sha256Hex(body)+`"`)
			w.WriteHeader(200)
			return
		}
		if r.Method == "POST" {
			if _, exists := f.objects[key]; exists && r.Header.Get("If-None-Match") == "*" {
				s3FixtureError(w, 412, "PreconditionFailed")
				return
			}
			numbers := []int{}
			for number := range upload.parts {
				numbers = append(numbers, number)
			}
			sort.Ints(numbers)
			var body []byte
			for _, number := range numbers {
				body = append(body, upload.parts[number]...)
			}
			etag := `"` + sha256Hex(body) + `"`
			f.objects[key] = s3FixtureObject{body, etag, upload.metadata}
			delete(f.uploads, id)
			s3XML(w, struct {
				XMLName           xml.Name `xml:"CompleteMultipartUploadResult"`
				Bucket, Key, ETag string
			}{Bucket: "fixture-bucket", Key: key, ETag: etag})
			return
		}
	}
	current, exists := f.objects[key]
	switch r.Method {
	case "PUT":
		if exists && r.Header.Get("If-None-Match") == "*" {
			s3FixtureError(w, 412, "PreconditionFailed")
			return
		}
		body, _ := io.ReadAll(r.Body)
		etag := `"` + sha256Hex(body) + `"`
		f.objects[key] = s3FixtureObject{body, etag, r.Header.Clone()}
		w.Header().Set("ETag", etag)
		w.WriteHeader(200)
	case "GET", "HEAD":
		if !exists {
			s3FixtureError(w, 404, "NoSuchKey")
			return
		}
		w.Header().Set("ETag", current.etag)
		w.Header().Set("Content-Length", strconv.Itoa(len(current.data)))
		for name, value := range current.metadata {
			if strings.HasPrefix(strings.ToLower(name), "x-amz-meta-") {
				w.Header()[name] = value
			}
		}
		if r.Method == "GET" {
			_, _ = w.Write(current.data)
		}
	case "DELETE":
		if exists && !f.ignoreConditions && r.Header.Get("If-Match") != "" && r.Header.Get("If-Match") != current.etag {
			s3FixtureError(w, 412, "PreconditionFailed")
			return
		}
		delete(f.objects, key)
		f.deleted = append(f.deleted, key)
		w.WriteHeader(204)
	default:
		s3FixtureError(w, 400, "InvalidRequest")
	}
}

func s3FixturePlan(t *testing.T) (*Config, *s3Fixture, *httptest.Server) {
	t.Helper()
	fixture := &s3Fixture{objects: map[string]s3FixtureObject{}, uploads: map[string]s3FixtureUpload{}}
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	access, secret, session := "fixture-access", "fixture-secret", "fixture-session"
	c := fixturePlan()
	c.ID = newID()
	c.ownerKey = make([]byte, 32)
	c.KeepLast = 1
	c.Destination = Destination{Kind: "s3", Bucket: "fixture-bucket", Endpoint: server.URL, Region: "us-east-1", Path: "snapshots", PathStyle: true, CredentialMode: "keys", AccessKey: &access, SecretKey: &secret, SessionToken: &session, Retention: true}
	return c, fixture, server
}

func s3ArtifactFixture(t *testing.T, c *Config, created time.Time) (string, string, archiveInfo) {
	t.Helper()
	payload := []byte("a disposable private backup fixture")
	file := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(file, payload, 0600); err != nil {
		t.Fatal(err)
	}
	return file, sha256Hex(payload), archiveInfo{Schema: "ervisio-backup-v1", JobID: c.ID, RunID: newID(), Created: created.UTC().Format(time.RFC3339), Mode: "folders", Files: 1}
}

func TestS3SignedUploadAndProvenanceRetentionPreserveForeignObjects(t *testing.T) {
	c, fixture, _ := s3FixturePlan(t)
	first, hash, info := s3ArtifactFixture(t, c, time.Now().Add(-48*time.Hour))
	old := backupArchiveName(c, info)
	if _, warnings, err := publishS3(context.Background(), c, first, hash, info); err != nil || len(warnings) > 0 {
		t.Fatal(warnings, err)
	}
	prefix := "snapshots/ervisio-backup-" + c.ID + "/"
	foreign := prefix + "foreign.txt"
	fixture.mu.Lock()
	fixture.objects[foreign] = s3FixtureObject{data: []byte("untouched"), etag: `"foreign"`}
	fixture.mu.Unlock()
	second, hash, info := s3ArtifactFixture(t, c, time.Now())
	if _, warnings, err := publishS3(context.Background(), c, second, hash, info); err != nil || len(warnings) > 0 {
		t.Fatal(warnings, err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if _, ok := fixture.objects[prefix+old]; ok {
		t.Fatal("expired owned archive remained")
	}
	if _, ok := fixture.objects[foreign]; !ok {
		t.Fatal("foreign S3 object deleted")
	}
	if fixture.signed == 0 {
		t.Fatal("AWS signing/session token was not used")
	}
}

func TestS3RefusesUnsafeConditionalDeleteAndOverwritingObjects(t *testing.T) {
	c, fixture, _ := s3FixturePlan(t)
	fixture.ignoreConditions = true
	first, hash, info := s3ArtifactFixture(t, c, time.Now().Add(-48*time.Hour))
	old := backupArchiveName(c, info)
	if _, warnings, err := publishS3(context.Background(), c, first, hash, info); err != nil || len(warnings) == 0 {
		t.Fatal("unsafe cleanup was not reported", warnings, err)
	}
	second, hash, info := s3ArtifactFixture(t, c, time.Now())
	if _, warnings, err := publishS3(context.Background(), c, second, hash, info); err != nil || len(warnings) == 0 {
		t.Fatal(warnings, err)
	}
	fixture.mu.Lock()
	_, exists := fixture.objects["snapshots/ervisio-backup-"+c.ID+"/"+old]
	fixture.mu.Unlock()
	if !exists {
		t.Fatal("unsafe S3 endpoint removed an existing archive")
	}
	c.Destination.Retention = false
	if _, _, err := publishS3(context.Background(), c, second, hash, info); err == nil {
		t.Fatal("overwrote an existing S3 object")
	}
}

func TestS3MultipartUploadAndFailedPartAbortsWithoutPublishing(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			c, fixture, _ := s3FixturePlan(t)
			c.Destination.Retention = false
			fixture.failParts = fail
			file, err := os.Create(filepath.Join(t.TempDir(), "large.tar.gz"))
			if err != nil {
				t.Fatal(err)
			}
			if err = file.Truncate(17 << 20); err != nil {
				t.Fatal(err)
			}
			_ = file.Close()
			info := archiveInfo{Schema: "ervisio-backup-v1", JobID: c.ID, RunID: newID(), Created: time.Now().UTC().Format(time.RFC3339)}
			_, _, err = publishS3(context.Background(), c, file.Name(), strings.Repeat("a", 64), info)
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fail {
				if err == nil || fixture.aborts != 1 || len(fixture.uploads) != 0 {
					t.Fatal("failed multipart upload was not aborted", err, fixture.aborts)
				}
				return
			}
			if err != nil || fixture.parts != 3 {
				t.Fatal("multipart upload failed", err, fixture.parts)
			}
			payload := fixture.objects["snapshots/ervisio-backup-"+c.ID+"/"+backupArchiveName(c, info)].data
			if len(payload) != 17<<20 || !bytes.Equal(payload, make([]byte, 17<<20)) {
				t.Fatal("multipart payload changed")
			}
		})
	}
}
