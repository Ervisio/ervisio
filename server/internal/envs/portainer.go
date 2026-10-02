package envs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/md5" //nolint:gosec // the Portainer agent's protocol hashes with MD5; not a security boundary here
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
)

// The Portainer agent authenticates the server that talks to it with two
// headers (portainer/agent: http/security/notary.go, crypto/ecdsa.go):
//
//	X-PortainerAgent-PublicKey  hex( DER PKIX of an ECDSA P-256 public key )
//	X-PortainerAgent-Signature  base64 (raw, no padding) of r||s, each
//	                            zero-padded to 32 bytes, an ECDSA signature
//	                            over md5(message)
//
// The message is the constant "Portainer-App", or the agent's AGENT_SECRET
// when the agent has one. Without a secret the agent binds itself to the
// first public key it sees (later requests must use the same key); with a
// secret it accepts any key whose signature covers the secret. So the
// secret is what makes the agent safe to expose; we always send our own key.
const (
	HeaderPublicKey = "X-PortainerAgent-PublicKey"
	HeaderSignature = "X-PortainerAgent-Signature"
	// AgentSignatureMessage is what the agent expects signed when it has no secret.
	AgentSignatureMessage = "Portainer-App"
)

// AgentSigner signs requests to Portainer agents.
type AgentSigner struct {
	key    *ecdsa.PrivateKey
	pubHex string
}

// NewAgentSigner generates a P-256 key.
func NewAgentSigner() (*AgentSigner, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return signerFor(k)
}

func signerFor(k *ecdsa.PrivateKey) (*AgentSigner, error) {
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		return nil, err
	}
	return &AgentSigner{key: k, pubHex: hex.EncodeToString(der)}, nil
}

// MarshalPEM returns the private key (to be sealed in the store).
func (s *AgentSigner) MarshalPEM() (string, error) {
	der, err := x509.MarshalECPrivateKey(s.key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})), nil
}

// ParseAgentSigner reads what MarshalPEM wrote.
func ParseAgentSigner(p string) (*AgentSigner, error) {
	b, _ := pem.Decode([]byte(p))
	if b == nil {
		return nil, errors.New("not PEM")
	}
	k, err := x509.ParseECPrivateKey(b.Bytes)
	if err != nil {
		return nil, err
	}
	if k.Curve != elliptic.P256() {
		return nil, errors.New("not a P-256 key")
	}
	return signerFor(k)
}

// PublicKeyHex is the value of the public key header.
func (s *AgentSigner) PublicKeyHex() string { return s.pubHex }

// Signature returns the signature header value for message (the agent
// secret, or AgentSignatureMessage when secret is empty).
func (s *AgentSigner) Signature(secret string) (string, error) {
	msg := AgentSignatureMessage
	if secret != "" {
		msg = secret
	}
	h := md5.Sum([]byte(msg)) //nolint:gosec
	r, ss, err := ecdsa.Sign(rand.Reader, s.key, h[:])
	if err != nil {
		return "", err
	}
	const size = 32
	out := make([]byte, 2*size)
	r.FillBytes(out[:size])
	ss.FillBytes(out[size:])
	return base64.RawStdEncoding.EncodeToString(out), nil
}

// Sign sets both headers on h.
func (s *AgentSigner) Sign(h http.Header, secret string) error {
	sig, err := s.Signature(secret)
	if err != nil {
		return err
	}
	h.Set(HeaderPublicKey, s.pubHex)
	h.Set(HeaderSignature, sig)
	return nil
}

// VerifyAgentSignature is the agent's check, ported from
// portainer/agent crypto/ecdsa.go: used by the tests (and a fake agent).
func VerifyAgentSignature(signature, keyHex, secret string) (bool, error) {
	der, err := hex.DecodeString(keyHex)
	if err != nil {
		return false, err
	}
	pk, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return false, err
	}
	pub, ok := pk.(*ecdsa.PublicKey)
	if !ok {
		return false, errors.New("not an ECDSA key")
	}
	raw, err := base64.RawStdEncoding.DecodeString(signature)
	if err != nil {
		return false, err
	}
	size := pub.Params().BitSize / 8
	if len(raw) != 2*size {
		return false, nil
	}
	msg := AgentSignatureMessage
	if secret != "" {
		msg = secret
	}
	h := md5.Sum([]byte(msg)) //nolint:gosec
	return ecdsa.Verify(pub, h[:], new(big.Int).SetBytes(raw[:size]), new(big.Int).SetBytes(raw[size:])), nil
}
