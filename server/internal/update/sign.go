package update

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// ReleasePublicKey is the ed25519 public key (base64) of the release key
// that signs SHA256SUMS of every GitHub release. It is separate from the
// plugin team key. The private key lives only on the maintainer's machine
// and in the RELEASE_SIGNING_KEY secret of the repository; see
// docs/RELEASING.md for rotation.
const ReleasePublicKey = "eYuaHtzKiE4ofn2sX/p7tSNX/p5+sGV9EJT0aKSmNU8="

// sigPrefix is prepended to SHA256SUMS before signing, so a release
// signature cannot be replayed as anything else (e.g. a plugin signature).
const sigPrefix = "linuxadmin-release-v1\n"

// Release file names.
const (
	SumsFile = "SHA256SUMS"
	SigFile  = "SHA256SUMS.sig"
)

// TrustedKeys are the release keys a SHA256SUMS signature may verify
// against. During a planned rotation the old key is listed here too.
var TrustedKeys = func() []ed25519.PublicKey {
	k, err := base64.StdEncoding.DecodeString(ReleasePublicKey)
	if err != nil || len(k) != ed25519.PublicKeySize {
		panic("update: bad ReleasePublicKey")
	}
	return []ed25519.PublicKey{k}
}()

// SignSums returns the signature file content (base64 + newline) for sums.
func SignSums(sums []byte, priv ed25519.PrivateKey) []byte {
	msg := append([]byte(sigPrefix), sums...)
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg)) + "\n")
}

// VerifySums checks the signature file content over sums.
func VerifySums(sums, sig []byte, keys []ed25519.PublicKey) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errors.New(SigFile + " is not a base64 ed25519 signature")
	}
	msg := append([]byte(sigPrefix), sums...)
	for _, k := range keys {
		if ed25519.Verify(k, msg, raw) {
			return nil
		}
	}
	return errors.New("the release signature does not match the LinuxAdmin release key")
}

var sumLineRe = regexp.MustCompile(`^([0-9a-f]{64}) [ *]([A-Za-z0-9][A-Za-z0-9._+-]{0,200})$`)

// ParseSums parses `sha256sum` output ("<hex>  <name>" per line). Names are
// plain file names; duplicates and malformed lines are errors.
func ParseSums(sums []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		m := sumLineRe.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("%s line %d is malformed", SumsFile, n)
		}
		if _, dup := out[m[2]]; dup {
			return nil, fmt.Errorf("%s lists %s twice", SumsFile, m[2])
		}
		out[m[2]] = m[1]
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New(SumsFile + " is empty")
	}
	return out, nil
}

// HashFile returns the hex sha256 of a file.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyFile checks that the file at path has the hash listed for name in
// signed sums (sums must already have been verified with VerifySums).
func VerifyFile(sums map[string]string, name, path string) error {
	want, ok := sums[name]
	if !ok {
		return fmt.Errorf("%s is not listed in %s", name, SumsFile)
	}
	got, err := HashFile(path)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%s does not match its sha256 in %s (download corrupted or tampered with)", name, SumsFile)
	}
	return nil
}
