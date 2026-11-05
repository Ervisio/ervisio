package plugins

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManifestErvisiOSPage(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(goodManifest), &doc); err != nil {
		t.Fatal(err)
	}
	for _, view := range []string{"desktop", "overview-os", "", "../view", "a/b", strings.Repeat("x", 33)} {
		t.Run(view, func(t *testing.T) {
			doc["contributes"].(map[string]any)["pages"] = []any{map[string]any{"id": "main", "title": "Main", "ervisios": map[string]any{"view": view}}}
			data, _ := json.Marshal(doc)
			m, err := ParseManifest(data)
			valid := view == "desktop" || view == "overview-os"
			if valid && (err != nil || m.Contributes.Pages[0].ErvisiOS.View != view) {
				t.Fatalf("native view lost: %v %v", m, err)
			}
			if !valid && err == nil {
				t.Fatal("invalid native view accepted")
			}
		})
	}
	// Existing plugins need no additional contribution or metadata.
	if _, err := ParseManifest([]byte(goodManifest)); err != nil {
		t.Fatal(err)
	}
}

func TestManifestErvisiOSOnlyOnPages(t *testing.T) {
	c := Contributes{Widgets: []Contribution{{ID: "main", Title: "Main", ErvisiOS: &ErvisiOSPage{View: "desktop"}}}}
	if err := c.validate(); err == nil {
		t.Fatal("desktop page metadata accepted on widget")
	}
}
