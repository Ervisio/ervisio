// Package signkey reads and writes the ed25519 key files used by the
// signing tools (plugin-sign for plugins, release-sign for releases).
//
// A key file holds one line: the base64 of the 64-byte ed25519 private key
// (what GenerateFile writes). The base64 of the 32-byte seed is accepted too.
package signkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxKeyFile bounds the size of a key file we accept to read.
const maxKeyFile = 4 << 10

// Parse decodes a base64 private key (64 bytes) or seed (32 bytes).
func Parse(text string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil {
		return nil, errors.New("not base64")
	}
	switch len(b) {
	case ed25519.PrivateKeySize:
		k := ed25519.PrivateKey(b)
		// The second half must be the public key of the first: catches
		// truncated or hand-edited files.
		if !k.Public().(ed25519.PublicKey).Equal(ed25519.NewKeyFromSeed(k.Seed()).Public()) {
			return nil, errors.New("corrupt private key")
		}
		return k, nil
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(b), nil
	}
	return nil, fmt.Errorf("expected a 32-byte seed or 64-byte private key, got %d bytes", len(b))
}

// Load reads a key file. It refuses files readable by group or others.
func Load(path string) (ed25519.PrivateKey, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is readable by other users (mode %v): chmod 600 it", path, fi.Mode().Perm())
	}
	if fi.Size() > maxKeyFile {
		return nil, fmt.Errorf("%s is not a key file", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	k, err := Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return k, nil
}

// GenerateFile creates a new key, writes it to path (mode 0600, parent
// folder created 0700, an existing file is never overwritten) and returns
// the public key.
func GenerateFile(path string) (ed25519.PublicKey, error) {
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(sk) + "\n"); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	return pk, nil
}

// ParsePublic decodes a base64 ed25519 public key.
func ParsePublic(text string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("not a base64 ed25519 public key")
	}
	return ed25519.PublicKey(b), nil
}
