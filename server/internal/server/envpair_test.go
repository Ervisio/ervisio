package server

import (
	"encoding/json"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"testing"
)

func TestPairAllowlistIsOnlyPluginCapabilities(t *testing.T) {
	for m := range pairAllowed {
		switch m {
		case "plugins.http", "plugins.httpStream", "plugins.exec", "plugins.execStream", "plugins.pty", "plugins.httpDownload", "plugins.httpUpload", "plugins.execDownload",
			"plugins.readFile", "plugins.writeFile", "plugins.listDir", "plugins.mkdir", "plugins.remove":
		default:
			t.Errorf("%s may not be reachable through a pairing", m)
		}
	}
	for _, m := range []string{"plugins.list", "plugins.install", "plugins.uninstall", "plugins.setEnabled", "plugins.loadDev", "plugins.readDir", "plugins.chmod",
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

func rpcMsg(method, params string) rpc.Message {
	return rpc.Message{ID: 1, Method: method, Params: json.RawMessage(params)}
}
