package software

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type nodeZeroReader struct{}

func (nodeZeroReader) Read(data []byte) (int, error) { clear(data); return len(data), nil }

func TestOfficialNodeExecutableAbove64MiBHasItsOwnBoundedAllowance(t *testing.T) {
	for _, name := range []string{"node/bin/node", "node/lib/node_modules/npm/oversized.js"} {
		archive := filepath.Join(t.TempDir(), "node.zip")
		file, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		writer := zip.NewWriter(file)
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0700)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.CopyN(entry, nodeZeroReader{}, 65<<20); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		dest := t.TempDir()
		err = unpackNode(archive, dest, "node")
		if strings.HasSuffix(name, "bin/node") {
			if err != nil {
				t.Fatal("official-sized executable refused", err)
			}
			fi, err := os.Stat(filepath.Join(dest, "bin", "node"))
			if err != nil || fi.Size() != 65<<20 {
				t.Fatal("executable was truncated", err)
			}
		} else if err == nil {
			t.Fatal("increased the limit for arbitrary npm files")
		}
	}
}

func TestNodeReleaseAndChecksums(t *testing.T) {
	index := []byte(`[{"version":"v99.0.0","lts":false,"files":["linux-x64"]},{"version":"../../escape","lts":"Bad","files":["linux-x64"]},{"version":"v26.13.0","lts":"Lithium","files":["linux-x64","win-arm64-zip"]}]`)
	version, name, err := nodeRelease(index, "linux", "amd64")
	if err != nil || version != "v26.13.0" || name != "node-v26.13.0-linux-x64.tar.gz" {
		t.Fatalf("stable platform selection: %s %s %v", version, name, err)
	}
	_, name, err = nodeRelease(index, "windows", "arm64")
	if err != nil || !strings.HasSuffix(name, "win-arm64.zip") {
		t.Fatalf("Windows ARM archive: %s %v", name, err)
	}
	if _, _, err := nodeRelease(index, "linux", "386"); err == nil {
		t.Fatal("unsupported architecture accepted")
	}
	sum := strings.Repeat("ab", 32)
	if got, err := nodeChecksum([]byte(sum+"  "+name+"\n"), name); err != nil || got != sum {
		t.Fatal("valid exact manifest entry refused", err)
	}
	for _, manifest := range []string{sum + "  other.zip\n", "short  " + name + "\n", strings.Repeat("zz", 32) + "  " + name + "\n"} {
		if _, err := nodeChecksum([]byte(manifest), name); err == nil {
			t.Fatalf("invalid checksum accepted: %s", manifest)
		}
	}
}

func nodeZipFixture(t *testing.T, name string, mode os.FileMode, content string) string {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	header := &zip.FileHeader{Name: name}
	header.SetMode(mode)
	file, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "node.zip")
	if err := os.WriteFile(archive, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestNodeArchiveExtractionRefusesTraversalAndLinks(t *testing.T) {
	for _, name := range []string{"node/../../outside", "/node/node.exe", `node\node.exe`, "node/C:/outside", "other/node.exe"} {
		t.Run(name, func(t *testing.T) {
			dest := t.TempDir()
			archive := nodeZipFixture(t, name, 0700, "payload")
			if err := unpackNode(archive, dest, "node"); err == nil {
				t.Fatalf("unsafe path accepted: %s", name)
			}
		})
	}
	dest := t.TempDir()
	archive := nodeZipFixture(t, "node/node.exe", 0700, "verified executable bytes")
	if err := unpackNode(archive, dest, "node"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "node.exe"))
	if err != nil || string(got) != "verified executable bytes" {
		t.Fatal("real extracted bytes differ", err)
	}
	dest = t.TempDir()
	archive = nodeZipFixture(t, "node/node.exe", os.ModeSymlink|0777, "/outside")
	if err := unpackNode(archive, dest, "node"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "node.exe")); !os.IsNotExist(err) {
		t.Fatal("archive created a symlink")
	}
}

func TestCLIRequestsAreAllowlistedAndUnelevated(t *testing.T) {
	for _, p := range []TxParams{{Op: "install", Packages: []string{"--evil"}}, {Op: "install", Packages: []string{"git"}}, {Op: "install", Packages: []string{"codex", "claude-code"}}, {Op: "exec", Packages: []string{"codex"}}} {
		if err := validateCLITx(p, false); err == nil {
			t.Fatalf("unsafe request accepted: %+v", p)
		}
	}
	if err := validateCLITx(TxParams{Op: "install", Packages: []string{"codex"}}, true); err == nil {
		t.Fatal("administrator CLI install accepted")
	}
	if !isRoot() {
		if err := validateCLITx(TxParams{Op: "install", Packages: []string{"codex"}}, false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCLICommandQuotesAccountMetacharacters(t *testing.T) {
	value := `/home/O'Brien $(touch nope);$HOME/node`
	quoted := cliQuote(value)
	if runtime.GOOS == "windows" {
		if quoted != "'"+strings.ReplaceAll(value, "'", "''")+"'" {
			t.Fatal("PowerShell literal quote changed")
		}
	} else if quoted != "'/home/O'\"'\"'Brien $(touch nope);$HOME/node'" {
		t.Fatalf("POSIX literal escaping: %s", quoted)
	}
	if got := ManagedCLIDirs(filepath.Join(t.TempDir(), "another-account")); len(got) != 0 {
		t.Fatalf("another account supplied managed tools: %v", got)
	}
}
