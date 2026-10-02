package server

import (
	"encoding/json"
	"testing"
)

func TestPairAllowlistIsOnlyPluginCapabilities(t *testing.T) {
	for m := range pairAllowed {
		switch m {
		case "plugins.http", "plugins.httpStream", "plugins.exec", "plugins.execStream", "plugins.pty", "plugins.httpDownload", "plugins.httpUpload", "plugins.execDownload":
		default:
			t.Errorf("%s may not be reachable through a pairing", m)
		}
	}
	for _, m := range []string{"plugins.list", "plugins.install", "plugins.uninstall", "plugins.setEnabled", "plugins.loadDev", "plugins.readFile", "plugins.writeFile",
		"plugins.network.approve", "plugins.envCheck", "files.list", "services.restart", "terminal.open", "config.set", "users.add"} {
		if pairAllowed[m] {
			t.Errorf("%s must not be allowed through a pairing", m)
		}
	}
}

func TestAddViaStripsEnvAndSetsVia(t *testing.T) {
	out := addVia(json.RawMessage(`{"plugin":"docker","name":"docker","env":"env-aabbccdd","envSocket":"/x","via":"spoofed"}`), "A by alice")
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["via"] != "A by alice" || m["env"] != "" || m["envSocket"] != "" || m["plugin"] != "docker" {
		t.Fatalf("%v", m)
	}
	// Not an object: left alone.
	if got := addVia(json.RawMessage(`[1]`), "x"); string(got) != `[1]` {
		t.Fatalf("%s", got)
	}
}
