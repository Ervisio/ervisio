package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDestinationValidationAndExternalRetentionDisabled(t *testing.T) {
	for _, kind := range []string{"http", "drive", "repo"} {
		c := fixturePlan()
		c.Folders = []string{t.TempDir()}
		c.Destination = Destination{Kind: kind, URL: "https://service.example", Owner: "team", Repository: "snapshots"}
		c.MaxAge = &Period{1, "days"}
		if err := validate(c); err != nil {
			t.Fatal(err)
		}
		if c.KeepLast != 0 || c.MaxAge != nil {
			t.Fatal("external service retention remained enabled")
		}
	}
	for _, d := range []Destination{{Kind: "http", URL: "https://user:secret@host"}, {Kind: "http", URL: "https://host?key=secret"}, {Kind: "http", URL: "https://host", APIHeader: "Host"}, {Kind: "http", URL: "https://host", Method: "DELETE"}, {Kind: "s3", Bucket: "bucket-name", Path: "../outside"}, {Kind: "s3", Bucket: "bucket-name", CredentialMode: "keys"}} {
		if validateDestination(&d, true) == nil {
			t.Fatalf("accepted unsafe destination: %s", d.Kind)
		}
	}
}

func TestDestinationSecretsStayEncryptedAndViewsDoNotMutateOriginal(t *testing.T) {
	s := store{dir: privateTempDir(t)}
	if err := s.init(); err != nil {
		t.Fatal(err)
	}
	c := fixturePlan()
	c.ID = newID()
	c.ownerKey = make([]byte, 32)
	api, access, secret, session := "api-private-value", "access-private-value", "secret-private-value", "session-private-value"
	c.Destination = Destination{Kind: "s3", Bucket: "bucket-name", CredentialMode: "keys", APIKey: &api, AccessKey: &access, SecretKey: &secret, SessionToken: &session}
	if err := s.write(c); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(s.dir, "state", c.ID+".json"))
	for _, value := range []string{api, access, secret, session} {
		if bytes.Contains(data, []byte(value)) {
			t.Fatal("plaintext destination secret")
		}
	}
	v := view(c)
	if v.Destination.APIKey != nil || v.Destination.SecretKey != nil || !v.Destination.HasCredentials || c.Destination.SecretKey == nil {
		t.Fatal("view leaked or erased credentials")
	}
	loaded, err := s.load(c.ID)
	if err != nil || *loaded.Destination.SecretKey != secret {
		t.Fatal("secret did not survive reload", err)
	}
}

func TestHTTPDriveAndRepoArtifactsUseRealUploadProtocolsWithoutDelete(t *testing.T) {
	for _, kind := range []string{"http", "drive", "repo"} {
		t.Run(kind, func(t *testing.T) {
			data := []byte("private export fixture\n")
			file := filepath.Join(t.TempDir(), "artifact")
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			artifact := uploadArtifact{Name: "fixture.sql", Local: file, Size: int64(len(data)), SHA256: sha256Hex(data), Manifest: []byte(`{"fixture":true}`)}
			releaseID := "11111111-2222-3333-4444-555555555555"
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method == http.MethodDelete {
					t.Fatal("HTTP adapter attempted retention")
				}
				if r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Error("missing API authorization")
				}
				if r.ContentLength < 0 {
					t.Error("upload needs Content-Length")
				}
				w.Header().Set("Content-Type", "application/json")
				if kind == "repo" && strings.HasSuffix(r.URL.Path, "/releases") {
					var input map[string]any
					if json.NewDecoder(r.Body).Decode(&input) != nil || input["draft"] != true {
						t.Error("repository snapshot was not a draft")
					}
					w.WriteHeader(201)
					_ = json.NewEncoder(w).Encode(map[string]any{"id": releaseID, "draft": true})
					return
				}
				if kind == "repo" {
					body, _ := io.ReadAll(r.Body)
					name := r.URL.Query().Get("name")
					if !strings.HasSuffix(r.URL.Path, "/assets") {
						t.Error("wrong raw asset URL")
					}
					w.WriteHeader(201)
					_ = json.NewEncoder(w).Encode(map[string]any{"name": name, "size": len(body), "sha256": sha256Hex(body)})
					return
				}
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				defer r.MultipartForm.RemoveAll()
				field := "file"
				if kind == "drive" {
					field = "files"
					if r.FormValue("folder") != "backups" || r.FormValue("expiresIn") != "" {
						t.Error("wrong Drive folder or automatic expiry")
					}
				}
				files := r.MultipartForm.File[field]
				if len(files) == 0 {
					t.Error("missing multipart file")
					w.WriteHeader(400)
					return
				}
				responses := []map[string]any{}
				for i, part := range files {
					f, _ := part.Open()
					body, _ := io.ReadAll(f)
					_ = f.Close()
					if i == 0 && !bytes.Equal(body, data) {
						t.Error("upload payload changed")
					}
					responses = append(responses, map[string]any{"_id": "1234567890abcdef12345678", "name": part.Filename, "size": len(body)})
				}
				w.WriteHeader(201)
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": responses})
			}))
			defer server.Close()
			key := "fixture-key"
			d := Destination{Kind: kind, URL: server.URL, APIKey: &key, Folder: "backups", Owner: "team", Repository: "code"}
			location, err := uploadHTTPArtifact(context.Background(), d, artifact)
			if err != nil || location == "" {
				t.Fatal(location, err)
			}
			if kind == "repo" && requests != 3 {
				t.Fatalf("expected draft + two asset requests, got %d", requests)
			}
		})
	}
}

func TestExternalUploadRefusesRedirectCredentialForwarding(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "artifact")
	_ = os.WriteFile(file, []byte("fixture"), 0600)
	key := "secret"
	_, err := uploadHTTPArtifact(context.Background(), Destination{Kind: "http", URL: server.URL, UploadMode: "raw", APIKey: &key}, uploadArtifact{Name: "one.txt", Local: file, Size: 7})
	if err == nil || forwarded {
		t.Fatal("followed an external redirect with credentials")
	}
}

func TestLocalPublicationRetentionAndOutputExclusion(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("account-owned worker supports Linux")
	}
	root := privateTempDir(t)
	source := filepath.Join(root, "source")
	_ = os.Mkdir(source, 0700)
	_ = os.WriteFile(filepath.Join(source, "keep.txt"), []byte("fixture"), 0600)
	c := fixturePlan()
	c.ID = newID()
	c.ownerKey = make([]byte, 32)
	c.Destination = Destination{Kind: "local", Path: source}
	c.Folders = []string{source}
	c.KeepLast = 1
	spool := privateTempDir(t)
	e := engine{store: store{dir: privateTempDir(t)}}
	if err := e.init(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		local, hash, info, err := buildArchive(context.Background(), c, newID(), spool, e.store.dir, &budget{limit: 8 << 20, dir: spool})
		if err != nil {
			t.Fatal(err)
		}
		// Distinct timestamps make the retention ordering deterministic.
		info.Created = time.Now().Add(time.Duration(i) * time.Minute).UTC().Format(time.RFC3339)
		archive, _, err := e.publishDestination(context.Background(), c, local, hash, info)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(archive)
		if err != nil {
			t.Fatal(err)
		}
		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		tr := tar.NewReader(gz)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(header.Name, "ervisio-backup-"+c.ID) {
				t.Fatal("backup included its own destination")
			}
		}
		_ = gz.Close()
		_ = f.Close()
		if err := os.Remove(local); err != nil {
			t.Fatal(err)
		}
	}
	namespace := filepath.Join(source, "ervisio-backup-"+c.ID)
	entries, _ := os.ReadDir(namespace)
	archives := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tar.gz") {
			archives++
		}
	}
	if archives != 1 {
		t.Fatalf("local retention kept %d archives", archives)
	}
	outside := filepath.Join(source, "outside.txt")
	_ = os.WriteFile(outside, []byte("untouched"), 0600)
	f, _, err := openLocalDestination(c)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Remove(context.Background(), "/outside.txt", false) == nil {
		t.Fatal("removed unrelated local file")
	}
	if _, err := os.Stat(path.Join(source, "keep.txt")); err != nil {
		t.Fatal("source was changed")
	}
}
