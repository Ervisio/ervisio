package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ervisio/ervisio/server/internal/signkey"
)

func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pk, sk
}

func sumLine(data []byte, name string) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]) + "  " + name + "\n"
}

func TestEmbeddedKeyParses(t *testing.T) {
	if len(TrustedKeys) == 0 || len(TrustedKeys[0]) != ed25519.PublicKeySize {
		t.Fatal("embedded release key missing")
	}
}

func TestSignVerify(t *testing.T) {
	pk, sk := testKey(t)
	otherPK, _ := testKey(t)
	sums := []byte(sumLine([]byte("a"), "ervisio-1.0.0-linux-amd64.tar.gz") + sumLine([]byte("b"), "ervisio-1.0.0-linux-arm64.tar.gz"))
	sig := SignSums(sums, sk)

	if err := VerifySums(sums, sig, []ed25519.PublicKey{pk}); err != nil {
		t.Fatalf("good signature rejected: %v", err)
	}
	if err := VerifySums(sums, sig, []ed25519.PublicKey{otherPK, pk}); err != nil {
		t.Fatalf("good signature with a second trusted key rejected: %v", err)
	}
	if err := VerifySums(sums, sig, []ed25519.PublicKey{otherPK}); err == nil {
		t.Fatal("signature by an untrusted key accepted")
	}
	tampered := append([]byte{}, sums...)
	tampered[0] ^= 1
	if err := VerifySums(tampered, sig, []ed25519.PublicKey{pk}); err == nil {
		t.Fatal("tampered SHA256SUMS accepted")
	}
	badSig := []byte(strings.Replace(string(sig), string(sig[0]), "A", 1))
	if string(badSig) == string(sig) {
		badSig = []byte("B" + string(sig[1:]))
	}
	if err := VerifySums(sums, badSig, []ed25519.PublicKey{pk}); err == nil {
		t.Fatal("tampered signature accepted")
	}
	for _, junk := range []string{"", "not base64!", "AAAA"} {
		if err := VerifySums(sums, []byte(junk), []ed25519.PublicKey{pk}); err == nil {
			t.Fatalf("junk signature %q accepted", junk)
		}
	}
	// A plugin-style signature (no release prefix) must not verify.
	if err := VerifySums(sums, []byte(b64(ed25519.Sign(sk, sums))), []ed25519.PublicKey{pk}); err == nil {
		t.Fatal("signature without the release prefix accepted")
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestParseSums(t *testing.T) {
	good := sumLine([]byte("x"), "a.tar.gz") + "\n" + strings.Replace(sumLine([]byte("y"), "b.tar.gz"), "  ", " *", 1)
	m, err := ParseSums([]byte(good))
	if err != nil || len(m) != 2 {
		t.Fatalf("got %v %v", m, err)
	}
	for _, bad := range []string{
		"",
		"zz  a\n",
		sumLine([]byte("x"), "../a"),
		sumLine([]byte("x"), "dir/a"),
		sumLine([]byte("x"), "a") + sumLine([]byte("y"), "a"),
		strings.ToUpper(sumLine([]byte("x"), "a")),
	} {
		if _, err := ParseSums([]byte(bad)); err == nil {
			t.Errorf("ParseSums(%q) accepted", bad)
		}
	}
}

func TestVerifyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	os.WriteFile(p, []byte("payload"), 0o644)
	sums, _ := ParseSums([]byte(sumLine([]byte("payload"), "f")))
	if err := VerifyFile(sums, "f", p); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p, []byte("payloaD"), 0o644)
	if err := VerifyFile(sums, "f", p); err == nil {
		t.Fatal("modified file accepted")
	}
	if err := VerifyFile(sums, "g", p); err == nil {
		t.Fatal("unlisted file accepted")
	}
}

func TestKeyFileRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "release.key")
	pk, err := signkey.GenerateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 { // no POSIX modes on Windows
		t.Fatalf("key file mode %v", fi.Mode().Perm())
	}
	if _, err := signkey.GenerateFile(p); err == nil {
		t.Fatal("existing key overwritten")
	}
	sk, err := signkey.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte(sumLine([]byte("a"), "a"))
	if err := VerifySums(sums, SignSums(sums, sk), []ed25519.PublicKey{pk}); err != nil {
		t.Fatal(err)
	}
	// The 32-byte seed form is accepted too.
	seed, err := signkey.Parse(b64(sk.Seed()))
	if err != nil || !seed.Equal(sk) {
		t.Fatalf("seed form: %v", err)
	}
	if runtime.GOOS != "windows" {
		os.Chmod(p, 0o644)
		if _, err := signkey.Load(p); err == nil {
			t.Fatal("world-readable key accepted")
		}
	}
}
