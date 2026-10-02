package sshauth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// MinRSABits is the smallest RSA modulus accepted.
const MinRSABits = 2048

// ErrUnsupportedKey means the key type or size is not accepted for sign-in.
var ErrUnsupportedKey = errors.New("unsupported key type")

// ErrBadSignature means the signature does not verify.
var ErrBadSignature = errors.New("signature verification failed")

// allowedFormats lists, per key type, the signature formats accepted.
// ssh-rsa (SHA-1) signatures are refused; FIDO (sk-*) and DSA keys cannot
// be used (a browser cannot talk to a security key through this flow,
// DSA is obsolete).
var allowedFormats = map[string][]string{
	ssh.KeyAlgoED25519:  {ssh.KeyAlgoED25519},
	ssh.KeyAlgoECDSA256: {ssh.KeyAlgoECDSA256},
	ssh.KeyAlgoECDSA384: {ssh.KeyAlgoECDSA384},
	ssh.KeyAlgoECDSA521: {ssh.KeyAlgoECDSA521},
	ssh.KeyAlgoRSA:      {ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512},
}

// ParsePublicKey parses one public key in authorized_keys form ("type
// base64 [comment]", no options) and checks that its type is accepted.
func ParsePublicKey(line string) (ssh.PublicKey, error) {
	line = strings.TrimSpace(line)
	if line == "" || len(line) > 16<<10 || strings.ContainsAny(line, "\n\r") {
		return nil, ErrUnsupportedKey
	}
	f := strings.Fields(line)
	if len(f) < 2 {
		return nil, ErrUnsupportedKey
	}
	if _, ok := allowedFormats[f[0]]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedKey, f[0])
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil {
		return nil, ErrUnsupportedKey
	}
	pub, err := ssh.ParsePublicKey(blob)
	if err != nil || pub.Type() != f[0] {
		return nil, ErrUnsupportedKey
	}
	if pub.Type() == ssh.KeyAlgoRSA {
		ck, ok := pub.(ssh.CryptoPublicKey)
		if !ok {
			return nil, ErrUnsupportedKey
		}
		rk, ok := ck.CryptoPublicKey().(*rsa.PublicKey)
		if !ok || rk.N.BitLen() < MinRSABits {
			return nil, fmt.Errorf("%w: RSA keys need at least %d bits", ErrUnsupportedKey, MinRSABits)
		}
	}
	return pub, nil
}

// ParseSignature decodes an SSH signature blob (string format, string
// blob), base64 encoded (standard or URL alphabet, padding optional).
func ParseSignature(b64 string) (*ssh.Signature, error) {
	if len(b64) > 8<<10 {
		return nil, ErrBadSignature
	}
	b64 = strings.TrimRight(strings.TrimSpace(b64), "=")
	raw, err := base64.RawStdEncoding.DecodeString(b64)
	if err != nil {
		if raw, err = base64.RawURLEncoding.DecodeString(b64); err != nil {
			return nil, ErrBadSignature
		}
	}
	format, rest, ok := sshString(raw)
	if !ok {
		return nil, ErrBadSignature
	}
	blob, rest, ok := sshString(rest)
	if !ok || len(rest) != 0 {
		return nil, ErrBadSignature
	}
	return &ssh.Signature{Format: string(format), Blob: blob}, nil
}

func sshString(b []byte) (s, rest []byte, ok bool) {
	if len(b) < 4 {
		return nil, nil, false
	}
	n := binary.BigEndian.Uint32(b)
	if uint64(n) > uint64(len(b)-4) {
		return nil, nil, false
	}
	return b[4 : 4+n], b[4+n:], true
}

// Verify checks sig over msg with pub, refusing signature formats not
// allowed for the key type (e.g. SHA-1 ssh-rsa).
func Verify(pub ssh.PublicKey, sig *ssh.Signature, msg []byte) error {
	ok := false
	for _, f := range allowedFormats[pub.Type()] {
		if sig.Format == f {
			ok = true
		}
	}
	if !ok || len(sig.Rest) != 0 {
		return fmt.Errorf("%w: format %q not accepted for %s keys", ErrBadSignature, sig.Format, pub.Type())
	}
	if err := pub.Verify(msg, sig); err != nil {
		return ErrBadSignature
	}
	return nil
}

// Fingerprint is the OpenSSH SHA256 fingerprint ("SHA256:…").
func Fingerprint(pub ssh.PublicKey) string { return ssh.FingerprintSHA256(pub) }
