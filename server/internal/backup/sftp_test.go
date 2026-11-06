package backup

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/modules/remote"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Every connection is loopback SSH/SFTP backed by memory, with credentials and
// keys generated for this test only. No external host or production data.
type fixturePermissionCommands struct {
	sftp.FileCmder
	mu    sync.Mutex
	modes map[string]uint32
}

func (f *fixturePermissionCommands) Filecmd(r *sftp.Request) error {
	// InMemHandler treats directories as files for Setstat and cannot chmod
	// them. Model a server accepting the real permission request explicitly.
	if r.Method == "Setstat" && r.AttrFlags().Permissions {
		f.mu.Lock()
		f.modes[r.Filepath] = r.Attributes().Mode & 0777
		f.mu.Unlock()
		return nil
	}
	return f.FileCmder.Filecmd(r)
}
func (f *fixturePermissionCommands) mode(p string) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.modes[p]
}

func savedSFTPFixture(t *testing.T) (string, *fixturePermissionCommands) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if meta.User() == "fixture" && string(password) == "fixture-backup-password" {
			return nil, nil
		}
		return nil, errors.New("fixture credentials rejected")
	}}
	serverConfig.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	handlers := sftp.InMemHandler()
	permissions := &fixturePermissionCommands{FileCmder: handlers.FileCmd, modes: map[string]uint32{}}
	handlers.FileCmd = permissions
	var mu sync.Mutex
	connections := []net.Conn{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			go func() {
				defer conn.Close()
				_, channels, requests, err := ssh.NewServerConn(conn, serverConfig)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					if incoming.ChannelType() != "session" {
						_ = incoming.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					ch, requests, err := incoming.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer ch.Close()
						for request := range requests {
							var sub struct{ Name string }
							ok := request.Type == "subsystem" && ssh.Unmarshal(request.Payload, &sub) == nil && sub.Name == "sftp"
							_ = request.Reply(ok, nil)
							if ok {
								server := sftp.NewRequestServer(ch, handlers)
								_ = server.Serve()
								_ = server.Close()
								return
							}
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range connections {
			_ = conn.Close()
		}
	})
	dir := filepath.Join(home, filepath.FromSlash(brand.UserDataDir))
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	storeKey := bytes.Repeat([]byte{7}, 32)
	block, _ := aes.NewCipher(storeKey)
	g, _ := cipher.NewGCM(block)
	nonce := make([]byte, g.NonceSize())
	_, _ = rand.Read(nonce)
	sealed := "v1:" + base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte("fixture-backup-password"), nil))
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	id := strings.Repeat("b", 16)
	data := map[string]any{"version": 1, "connections": []any{map[string]any{"id": id, "name": "Fixture SFTP", "kind": "sftp", "host": host, "port": port, "user": "fixture", "auth": "password", "hostKey": strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), "path": "/", "sealed": map[string]string{"password": sealed}}}}
	b, _ := json.Marshal(data)
	if err = os.WriteFile(filepath.Join(dir, "remotes.key"), storeKey, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "remotes.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	return id, permissions
}

func TestActualSFTPAtomicBackupAndSavedCredentialIsolation(t *testing.T) {
	id, permissions := savedSFTPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	views, err := remote.BackupConnections()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(views)
	if len(views) != 1 || !views[0].HasPassword || bytes.Contains(encoded, []byte("fixture-backup-password")) || bytes.Contains(encoded, []byte("sealed")) {
		t.Fatal("remote connection view leaked credentials or omitted real connection")
	}
	c := fixturePlan()
	c.Destination.Connection = id
	local := filepath.Join(t.TempDir(), "archive")
	os.WriteFile(local, []byte("owned-real-sftp-archive"), 0600)
	var published string
	err = remote.WithBackupConnection(ctx, id, func(ctx context.Context, f remote.FS) error {
		info := archiveInfo{Schema: "ervisio-backup-v1", JobID: c.ID, RunID: newID(), Created: time.Now().UTC().Format(time.RFC3339), Mode: "folders"}
		var warnings []string
		var err error
		published, warnings, err = publish(ctx, f, c, local, strings.Repeat("0", 64), info)
		if err != nil {
			return err
		}
		if len(warnings) > 0 {
			t.Fatalf("unexpected cleanup warnings: %v", warnings)
		}
		r, err := f.Open(ctx, published)
		if err != nil {
			return err
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return err
		}
		if string(b) != "owned-real-sftp-archive" {
			t.Fatal("real uploaded archive differs")
		}
		if _, err = f.Stat(ctx, published+".manifest.json"); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if published == "" {
		t.Fatal("no published archive")
	}
	if permissions.mode("/backups/ervisio-backup-"+c.ID) != 0700 {
		t.Fatal("SFTP namespace was not restricted to 0700")
	}
	permissions.mu.Lock()
	defer permissions.mu.Unlock()
	privateFiles := 0
	for p, mode := range permissions.modes {
		if strings.Contains(p, ".ervisio-upload-") {
			privateFiles++
			if mode != 0600 {
				t.Fatal("SFTP temporary data file was not private before upload")
			}
		}
	}
	if privateFiles < 3 {
		t.Fatal("missing archive, manifest and marker permission requests")
	}
}

func TestEndToEndFolderWorkerUsesRealSFTPAndRecordsUndoSafeHistory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux account worker")
	}
	id, _ := savedSFTPFixture(t)
	e := &engine{store: store{dir: t.TempDir()}}
	if err := e.init(); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	os.WriteFile(filepath.Join(source, "application.json"), []byte(`{"fixture":true}`), 0600)
	c := fixturePlan()
	c.ID = ""
	c.Folders = []string{source}
	c.Destination.Connection = id
	if err := e.store.save(c); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	receipt, err := e.run(ctx, c.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "ok" || receipt.Files != 1 || receipt.SHA256 == "" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	history, err := e.store.history(c.ID)
	if err != nil || len(history) != 1 || history[0].Status != "ok" {
		t.Fatalf("history not persisted: %v %v", history, err)
	}
	entries, _ := os.ReadDir(e.store.dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "spool-") {
			t.Fatal("private spool not removed")
		}
	}
}
