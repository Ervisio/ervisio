package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDatabaseDeliveryConfinesPartsAndNeverReplacesFiles(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("user-level Linux delivery")
	}
	parent := privateTempDir(t)
	e := engine{store: store{dir: filepath.Join(parent, "backup")}}
	if err := e.init(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(parent, "database", "exports")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	destination := privateTempDir(t)
	send := func(name, part string) (any, error) {
		token := newID()
		if err := os.WriteFile(filepath.Join(source, token+"-0000.part"), []byte(part), 0600); err != nil {
			t.Fatal(err)
		}
		request := deliveryRequest{Destination: Destination{Kind: "local", Path: destination}, Name: name, Parts: 1, Bytes: int64(len(part))}
		data, _ := json.Marshal(request)
		if err := os.WriteFile(filepath.Join(e.store.dir, "inbox", token+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		return e.deliver(context.Background(), token)
	}
	if _, err := send("report.csv", "name\nalice\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := send("report.csv", "replacement"); err == nil {
		t.Fatal("replaced an existing export")
	}
	bytes, err := os.ReadFile(filepath.Join(destination, "report.csv"))
	if err != nil || string(bytes) != "name\nalice\n" {
		t.Fatal("existing export changed", err)
	}
	for _, name := range []string{"../escape.csv", "/outside.csv", "bad\nname"} {
		if _, err := send(name, "data"); err == nil {
			t.Fatal("accepted invalid name", name)
		}
	}
	token := newID()
	if err := os.Symlink(filepath.Join(destination, "report.csv"), filepath.Join(source, token+"-0000.part")); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(deliveryRequest{Destination: Destination{Kind: "local", Path: destination}, Name: "symlink.csv", Parts: 1, Bytes: 11})
	_ = os.WriteFile(filepath.Join(e.store.dir, "inbox", token+".json"), data, 0600)
	if _, err := e.deliver(context.Background(), token); err == nil {
		t.Fatal("followed an export symlink")
	}
	entries, _ := os.ReadDir(e.store.dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "delivery-") {
			t.Fatal("left export spool behind")
		}
	}
}
