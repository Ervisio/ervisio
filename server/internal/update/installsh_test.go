package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// install.sh (repository root) verifies releases with openssl and an
// embedded copy of the release key; the copy must be the key the consoles
// trust.
func TestInstallScriptReleaseKey(t *testing.T) {
	script := readInstallScript(t)
	blocks := pemBlocks(t, script, "RELEASE_KEY_PEM")
	if len(blocks) != 1 {
		t.Fatalf("install.sh: want 1 RELEASE_KEY_PEM block, found %d", len(blocks))
	}
	pub, err := x509.ParsePKIXPublicKey(blocks[0].Bytes)
	if err != nil {
		t.Fatal(err)
	}
	k, ok := pub.(ed25519.PublicKey)
	if !ok {
		t.Fatalf("install.sh: release key is %T, not ed25519", pub)
	}
	if !k.Equal(TrustedKeys[0]) {
		t.Fatal("install.sh: RELEASE_KEY_PEM is not ReleasePublicKey (sign.go); update both together")
	}
	// The signed-message prefix must match too.
	if !strings.Contains(script, "printf 'linuxadmin-release-v1\\n'") || sigPrefix != "linuxadmin-release-v1\n" {
		t.Fatal("install.sh: signature prefix differs from sigPrefix")
	}
}

// install.sh carries copies of packaging/pam.d/* for release archives that
// do not ship them (0.1.0); they must not drift.
func TestInstallScriptPAMCopies(t *testing.T) {
	script := readInstallScript(t)
	files, _ := filepath.Glob(filepath.Join(repoRoot(t), "packaging", "pam.d", "ervisio.*"))
	if len(files) < 4 {
		t.Fatalf("packaging/pam.d has %d variants", len(files))
	}
	for _, f := range files {
		fam := strings.TrimPrefix(filepath.Ext(f), ".")
		raw, _ := os.ReadFile(f)
		want := strings.ReplaceAll(string(raw), "\r\n", "\n") // CRLF checkout on Windows
		re := regexp.MustCompile(`(?s)\n` + fam + `\)\n\s*cat <<'PAM'\n(.*?)\nPAM\n`)
		m := re.FindStringSubmatch(script)
		if m == nil {
			t.Errorf("install.sh has no embedded PAM file for %s", fam)
			continue
		}
		if strings.TrimSpace(m[1]) != strings.TrimSpace(want) {
			t.Errorf("install.sh: embedded PAM file for %s differs from %s", fam, f)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readInstallScript(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout may have CRLF line endings; install.sh is parsed as LF.
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// pemBlocks returns the PEM blocks of the shell variable name='...'.
func pemBlocks(t *testing.T, script, name string) []*pem.Block {
	t.Helper()
	m := regexp.MustCompile(`(?s)\n` + name + `='(.*?)'\n`).FindStringSubmatch(script)
	if m == nil {
		t.Fatalf("install.sh: no %s='...' variable", name)
	}
	var out []*pem.Block
	rest := []byte(m[1])
	for {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		out = append(out, b)
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		t.Fatalf("install.sh: junk after the PEM block in %s", name)
	}
	return out
}
