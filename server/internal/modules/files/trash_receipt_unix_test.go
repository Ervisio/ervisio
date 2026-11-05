//go:build unix

package files

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func TestTrashReceiptsIdentifyRepeatedNamesAndRestoreExactly(t *testing.T) {
	home := t.TempDir()
	oldHome := homeOverride
	homeOverride = home
	t.Cleanup(func() { homeOverride = oldHome })
	path := filepath.Join(home, "report.txt")
	deleteFile := func(content string) string {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := doDelete([]string{path}, true)
		if err != nil {
			t.Fatal(err)
		}
		receipts := result.(map[string]any)["trashed"].([]map[string]string)
		if len(receipts) != 1 || receipts[0]["path"] != path || receipts[0]["id"] == "" {
			t.Fatalf("missing stable trash receipt: %v", result)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("source was not moved to trash")
		}
		return receipts[0]["id"]
	}
	first := deleteFile("first deletion")
	second := deleteFile("second deletion")
	if first == second {
		t.Fatal("same-name deletions reused a trash receipt")
	}
	params, _ := json.Marshal(map[string]any{"ids": []string{first}})
	if _, err := hRestore(context.Background(), &rpc.Call{Params: params}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "first deletion" {
		t.Fatalf("restored a different deletion: %q %v", content, err)
	}
	filesDir, _, _ := trashDirs()
	remaining, err := os.ReadFile(filepath.Join(filesDir, second))
	if err != nil || string(remaining) != "second deletion" {
		t.Fatal("undo touched the other receipt")
	}
}
