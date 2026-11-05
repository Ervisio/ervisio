package plugins

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ervisio/ervisio/server/internal/update"
)

// TeamPublicKey is the ed25519 public key (base64) of the Ervisio team key
// that signs first-party plugins. The private key never lives in the
// repository; see docs/PLUGIN-SIGNING.md for where it is kept and how to
// rotate it.
const TeamPublicKey = "reTBt4sr4E7AYindYYzscF4oUSPzf1hSQ7d8f/R2BfU="

// signaturePrefix is prepended to the canonical manifest before signing, so
// a signature cannot be replayed for another purpose.
//
// Historical constant: it keeps the product's former name (LinuxAdmin) on
// purpose. Every plugin signed so far (manifest.sig) and the signed catalog
// were made over it, and consoles still running LinuxAdmin verify new
// signatures with it; changing it would invalidate all of them at once.
const signaturePrefix = "linuxadmin-plugin-v1\n"

// TrustedKeys are the keys a signature may verify against.
var TrustedKeys = update.MustParseKeys(TeamPublicKey, "plugins: bad TeamPublicKey")

// Signature describes the signature state of a plugin folder.
type Signature struct {
	// Signed: a manifest.sig exists.
	Signed bool
	// Verified: the signature is valid for a trusted key and every file matches its hash.
	Verified bool
	// Err explains why a present signature did not verify.
	Err string
}

// Canonical returns the canonical form of a manifest: the JSON re-encoded
// with sorted keys, no whitespace, no HTML escaping.
func Canonical(manifest []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(manifest))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil { // maps are encoded with sorted keys
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// prefixedCanonical is the byte string a signature covers: prefix and the
// canonical form of doc.
func prefixedCanonical(prefix string, doc []byte) ([]byte, error) {
	c, err := Canonical(doc)
	if err != nil {
		return nil, err
	}
	return append([]byte(prefix), c...), nil
}

// SigningMessage is the exact byte string that is signed: a prefix and the
// canonical manifest. The manifest's `files` map carries the sha256 of every
// other file, so the signature covers them too.
func SigningMessage(manifest []byte) ([]byte, error) {
	return prefixedCanonical(signaturePrefix, manifest)
}

// signedDoc names one kind of signed document and the messages its
// verification reports.
type signedDoc struct {
	message func([]byte) ([]byte, error)
	sigFile string
	docFile string
	noMatch string
}

// verify checks sig (base64) over doc with the given keys.
func (k signedDoc) verify(doc, sig []byte, keys []ed25519.PublicKey) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errors.New(k.sigFile + " is not a base64 ed25519 signature")
	}
	msg, err := k.message(doc)
	if err != nil {
		return fmt.Errorf("%s: %v", k.docFile, err)
	}
	for _, key := range keys {
		if ed25519.Verify(key, msg, raw) {
			return nil
		}
	}
	return errors.New(k.noMatch)
}

var manifestSig = signedDoc{
	message: SigningMessage,
	sigFile: "manifest.sig",
	docFile: "manifest.json",
	noMatch: "the signature does not match any trusted key",
}

// VerifySignature checks sig (base64) over the manifest bytes with the given keys.
func VerifySignature(manifest, sig []byte, keys []ed25519.PublicKey) error {
	return manifestSig.verify(manifest, sig, keys)
}

// HashFolder returns sha256 (hex) of every regular file under dir except
// manifest.json and manifest.sig, keyed by slash-separated relative path.
// Symlinks and special files are an error.
func HashFolder(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if rel == "manifest.json" || rel == "manifest.sig" {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
		h, err := update.HashFile(p)
		if err != nil {
			return err
		}
		out[rel] = h
		return nil
	})
	return out, err
}

// CheckSignature inspects dir for manifest.sig and verifies it against keys:
// signature over the canonical manifest, and every listed file has the
// declared hash, and no unlisted file is present.
func CheckSignature(dir string, keys []ed25519.PublicKey) Signature {
	sig, err := os.ReadFile(filepath.Join(dir, "manifest.sig"))
	if err != nil {
		return Signature{}
	}
	s := Signature{Signed: true}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		s.Err = "manifest.json is missing"
		return s
	}
	if err := VerifySignature(raw, sig, keys); err != nil {
		s.Err = err.Error()
		return s
	}
	m, err := ParseManifest(raw)
	if err != nil {
		s.Err = err.Error()
		return s
	}
	if len(m.Files) == 0 {
		s.Err = "a signed manifest must list its files with sha256 hashes"
		return s
	}
	actual, err := HashFolder(dir)
	if err != nil {
		s.Err = err.Error()
		return s
	}
	if s.Err = fileMismatch(m.Files, actual); s.Err == "" {
		s.Verified = true
	}
	return s
}

// fileMismatch compares the hashes declared in the manifest with the actual
// ones and describes the first difference, or returns "" when they agree.
func fileMismatch(declared, actual map[string]string) string {
	names := make([]string, 0, len(actual))
	for p := range actual {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		want, ok := declared[p]
		if !ok {
			return fmt.Sprintf("%s is not listed in the signed manifest", p)
		}
		if want != actual[p] {
			return fmt.Sprintf("%s was changed after signing", p)
		}
	}
	for p := range declared {
		if _, ok := actual[p]; !ok {
			return fmt.Sprintf("%s is listed in the manifest but missing", p)
		}
	}
	return ""
}

// SignFolder fills the manifest's `files` map from the folder, rewrites
// manifest.json and writes manifest.sig. Used by cmd/plugin-sign.
func SignFolder(dir string, priv ed25519.PrivateKey) error {
	mp := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(mp)
	if err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("manifest.json: %v", err)
	}
	files, err := HashFolder(dir)
	if err != nil {
		return err
	}
	fm := map[string]any{}
	for p, h := range files {
		fm[p] = h
	}
	obj["files"] = fm
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(obj); err != nil {
		return err
	}
	if _, err := ParseManifest(buf.Bytes()); err != nil {
		return err
	}
	if err := os.WriteFile(mp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	msg, err := SigningMessage(buf.Bytes())
	if err != nil {
		return err
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg))
	return os.WriteFile(filepath.Join(dir, "manifest.sig"), []byte(sig+"\n"), 0o644)
}
