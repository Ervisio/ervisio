package remote

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// This is a real SSH/SFTP endpoint backed by memory, never an external host.
func sftpFixture(t *testing.T) (string, int, ssh.PublicKey, *atomic.Int32) {
	t.Helper()
	return sftpFixtureWithStatVFSError(t, sftp.ErrSSHFxOpUnsupported)
}

type fixtureFileCmder struct {
	sftp.FileCmder
	statVFSError error
}

func (f fixtureFileCmder) StatVFS(*sftp.Request) (*sftp.StatVFS, error) {
	return nil, f.statVFSError
}

func sftpFixtureWithStatVFSError(t *testing.T, statVFSError error) (string, int, ssh.PublicKey, *atomic.Int32) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	auths := &atomic.Int32{}
	config := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		auths.Add(1)
		if meta.User() == "fixture" && string(password) == "fixture-test-secret" {
			return nil, nil
		}
		return nil, errors.New("incorrect fixture credentials")
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	handlers := sftp.InMemHandler()
	// InMemHandler.StatVFS queries the host OS rather than its in-memory tree.
	// Explicit protocol responses keep this fixture independent of host paths
	// and allow real missing/permission errors to remain distinguishable.
	handlers.FileCmd = fixtureFileCmder{FileCmder: handlers.FileCmd, statVFSError: statVFSError}
	var mu sync.Mutex
	var connections []net.Conn
	closed := make(chan struct{})
	go func() {
		defer close(closed)
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
				_, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					if channel.ChannelType() != "session" {
						_ = channel.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					ch, reqs, err := channel.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer ch.Close()
						for request := range reqs {
							var subsystem struct{ Name string }
							ok := request.Type == "subsystem" && ssh.Unmarshal(request.Payload, &subsystem) == nil && subsystem.Name == "sftp"
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
		<-closed
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range connections {
			_ = conn.Close()
		}
	})
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return host, port, signer.PublicKey(), auths
}

func remoteCall(params any) *rpc.Call {
	b, _ := json.Marshal(params)
	return &rpc.Call{Params: b}
}

type remoteTestStream struct {
	bytes  bytes.Buffer
	events []json.RawMessage
	input  chan json.RawMessage
}

func (s *remoteTestStream) Send(v any) error {
	b, _ := json.Marshal(v)
	s.events = append(s.events, b)
	return nil
}
func (s *remoteTestStream) SendBytes(b []byte) error      { _, err := s.bytes.Write(b); return err }
func (s *remoteTestStream) Input() <-chan json.RawMessage { return s.input }

func TestSFTPTrustTransfersAndSavedCredentials(t *testing.T) {
	host, port, serverKey, auths := sftpFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st := &store{dir: t.TempDir()}
	m := &module{st: st, pool: newPool(st)}
	spec := map[string]any{"name": "Fixture", "kind": "sftp", "host": host, "port": port, "user": "fixture", "auth": "password", "password": "fixture-test-secret", "path": "/"}
	result, err := m.hTest(ctx, remoteCall(spec))
	if err != nil {
		t.Fatal(err)
	}
	trust := result.(map[string]any)
	if trust["needsTrust"] != true || trust["fingerprint"] != fingerprint(serverKey) {
		t.Fatalf("unverified endpoint did not require explicit trust: %v", trust)
	}
	if auths.Load() != 0 {
		t.Fatal("password sent before host identity was verified")
	}
	_, otherPrivate, _ := ed25519.GenerateKey(rand.Reader)
	otherSigner, _ := ssh.NewSignerFromKey(otherPrivate)
	spec["hostKey"] = formatHostKey(otherSigner.PublicKey())
	changed, err := m.hTest(ctx, remoteCall(spec))
	if err != nil || changed.(map[string]any)["changed"] != true || auths.Load() != 0 {
		t.Fatalf("a changed host key was not stopped before authentication: %v", err)
	}
	delete(spec, "hostKey")
	if _, err := m.hSave(ctx, remoteCall(spec)); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatalf("untrusted save allowed: %v", err)
	}
	spec["hostKey"] = trust["hostKey"]
	if _, err := m.hTest(ctx, remoteCall(spec)); err != nil {
		t.Fatal(err)
	}
	saved, err := m.hSave(ctx, remoteCall(spec))
	if err != nil {
		t.Fatal(err)
	}
	view := saved.(View)
	defer m.pool.drop(view.ID)
	encoded, _ := json.Marshal(view)
	if bytes.Contains(encoded, []byte("fixture-test-secret")) || !view.HasPassword {
		t.Fatal("saved connection exposed its password")
	}
	disk, err := os.ReadFile(filepath.Join(st.dir, "remotes.json"))
	if err != nil || bytes.Contains(disk, []byte("fixture-test-secret")) {
		t.Fatalf("credential not sealed on disk: %v", err)
	}
	if _, err := m.hMkdir(ctx, remoteCall(map[string]any{"conn": view.ID, "path": "/app"})); err != nil {
		t.Fatal(err)
	}
	if _, err := m.hWriteText(ctx, remoteCall(map[string]any{"conn": view.ID, "path": "/app/config.yml", "content": "port: 8080\n", "encoding": "utf-8"})); err != nil {
		t.Fatal(err)
	}
	read, err := m.hReadText(ctx, remoteCall(map[string]any{"conn": view.ID, "path": "/app/config.yml"}))
	if err != nil || read.(map[string]any)["content"] != "port: 8080\n" {
		t.Fatalf("text roundtrip failed: %v", err)
	}
	capacity, err := m.hStatfs(ctx, remoteCall(map[string]any{"conn": view.ID, "path": "/app"}))
	if err != nil || capacity.(Capacity).Supported {
		t.Fatalf("unsupported SFTP statvfs should be honestly unknown: %v %v", capacity, err)
	}
	part := &remoteTestStream{}
	if err := m.hReadStream(ctx, remoteCall(map[string]any{"conn": view.ID, "path": "/app/config.yml", "offset": 6, "length": 4}), part); err != nil || part.bytes.String() != "8080" {
		t.Fatalf("SFTP range read: %q %v", part.bytes.String(), err)
	}
	probe := &remoteTestStream{}
	if err := m.hReadStream(ctx, remoteCall(map[string]any{"conn": view.ID, "path": "/app/config.yml", "metadataOnly": true}), probe); err != nil || probe.bytes.Len() != 0 {
		t.Fatalf("SFTP metadata probe transferred bytes: %d %v", probe.bytes.Len(), err)
	}
	search := &remoteTestStream{}
	if err := m.hSearchText(ctx, remoteCall(map[string]any{"conn": view.ID, "root": "/app", "query": "8080"}), search); err != nil {
		t.Fatal(err)
	}
	searchData, _ := json.Marshal(search.events)
	if !bytes.Contains(searchData, []byte(`"path":"/app/config.yml"`)) || !bytes.Contains(searchData, []byte(`"line":1`)) {
		t.Fatalf("remote project search did not return file and line: %s", searchData)
	}
	if _, err := m.hWriteText(ctx, remoteCall(map[string]any{"conn": view.ID, "path": "/app/config.yml", "content": "bad", "expectedMtime": 1})); !rpc.IsCode(err, rpc.Conflict) {
		t.Fatalf("stale edit was not refused: %v", err)
	}
	upload := &remoteTestStream{input: make(chan json.RawMessage, 2)}
	upload.input <- json.RawMessage(`{"data":"` + base64.StdEncoding.EncodeToString([]byte("uploaded content")) + `"}`)
	upload.input <- json.RawMessage(`{"eof":true}`)
	if err := m.hWriteStream(ctx, remoteCall(map[string]any{"conn": view.ID, "path": "/app/upload.txt", "size": 16}), upload); err != nil {
		t.Fatal(err)
	}
	download := &remoteTestStream{}
	if err := m.hReadStream(ctx, remoteCall(map[string]any{"conn": view.ID, "path": "/app/upload.txt"}), download); err != nil {
		t.Fatal(err)
	}
	if download.bytes.String() != "uploaded content" {
		t.Fatal("SFTP upload/download bytes differ")
	}
	localDir := t.TempDir()
	copyStream := &remoteTestStream{}
	if err := m.hCopy(ctx, remoteCall(map[string]any{"from": map[string]any{"conn": view.ID, "paths": []string{"/app/upload.txt"}}, "to": map[string]any{"conn": "", "path": localDir}}), copyStream); err != nil {
		t.Fatal(err)
	}
	localBytes, err := os.ReadFile(filepath.Join(localDir, "upload.txt"))
	if err != nil || string(localBytes) != "uploaded content" {
		t.Fatalf("remote-to-local copy failed: %v", err)
	}
	if _, err := m.hForget(ctx, remoteCall(map[string]string{"id": view.ID})); err != nil {
		t.Fatal(err)
	}
	if _, err := st.get(view.ID); !rpc.IsCode(err, rpc.NotFound) {
		t.Fatalf("forgotten credentials still available: %v", err)
	}
}

func TestSFTPCapacityPreservesRemoteErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code rpc.Code
	}{
		{"missing", sftp.ErrSSHFxNoSuchFile, rpc.NotFound},
		{"permission", sftp.ErrSSHFxPermissionDenied, rpc.Forbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, port, serverKey, _ := sftpFixtureWithStatVFSError(t, tc.err)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			st := &store{dir: t.TempDir()}
			m := &module{st: st, pool: newPool(st)}
			saved, err := m.hSave(ctx, remoteCall(map[string]any{
				"name": "Fixture", "kind": "sftp", "host": host, "port": port,
				"user": "fixture", "auth": "password", "password": "fixture-test-secret",
				"path": "/", "hostKey": formatHostKey(serverKey),
			}))
			if err != nil {
				t.Fatal(err)
			}
			view := saved.(View)
			defer m.pool.drop(view.ID)
			if _, err := m.hStatfs(ctx, remoteCall(map[string]any{"conn": view.ID, "path": "/app"})); !rpc.IsCode(err, tc.code) {
				t.Fatalf("capacity must preserve %s instead of reporting unsupported: %v", tc.code, err)
			}
		})
	}
}
