package plugins

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Environments (remote) and user-approved hosts.

const envManifest = `{
  "id": "envp", "name": "Env plugin", "version": "1.0.0", "entry": "index.js", "platforms": ["linux", "windows"],
  "capabilities": {
    "commands": [
      {"name": "ps", "argv": ["docker", "-H", "{env}", "ps"], "admin": false, "remote": "docker"},
      {"name": "local", "argv": ["echo", "local"], "admin": false}
    ],
    "http": [
      {"name": "docker", "socket": "%SOCK%", "admin": true, "remote": "docker",
       "rules": [{"methods": ["GET", "POST"], "path": "/version"}]},
      {"name": "other", "socket": "%SOCK%", "admin": false, "rules": [{"methods": ["GET"], "path": "/version"}]}
    ],
    "files": {"read": [], "write": ["/opt/stacks"]},
    "sockets": [], "network": {"hosts": ["api.example.org"], "userHosts": true}
  },
  "contributes": {"pages": [], "widgets": [], "snippets": []},
  "visibleTo": {"groups": []}
}`

func TestManifestRemoteAndNetwork(t *testing.T) {
	m, err := ParseManifest([]byte(strings.ReplaceAll(envManifest, "%SOCK%", "/run/x.sock")))
	if err != nil {
		t.Fatal(err)
	}
	c := m.Capabilities
	if !c.UserHosts || len(c.Network) != 1 || c.Network[0] != "api.example.org" || c.HTTP[0].Remote != "docker" || c.Commands[0].Remote != "docker" {
		t.Fatalf("%+v", c)
	}
	// the list form still works and has no userHosts
	old, err := ParseManifest([]byte(goodManifest))
	if err != nil || old.Capabilities.UserHosts {
		t.Fatalf("old manifest: %v", err)
	}
	// consent round trip keeps userHosts
	raw, _ := json.Marshal(c)
	var back Capabilities
	if err := json.Unmarshal(raw, &back); err != nil || !back.UserHosts || len(back.Network) != 1 {
		t.Fatalf("round trip: %v %+v", err, back)
	}
	base := strings.ReplaceAll(envManifest, "%SOCK%", "/run/x.sock")
	bad := map[string]string{
		"unknown remote family":  strings.Replace(base, `"remote": "docker"}`, `"remote": "k8s"}`, 1),
		"remote without {env}":   strings.Replace(base, `["docker", "-H", "{env}", "ps"]`, `["echo", "ps"]`, 1),
		"{env} without remote":   strings.Replace(base, `["echo", "local"]`, `["echo", "{env}"]`, 1),
		"{env} twice":            strings.Replace(base, `"{env}", "ps"`, `"{env}", "{env}"`, 1),
		"{env} inside an item":   strings.Replace(base, `"{env}"`, `"-H{env}"`, 1),
		"{env} as the program":   strings.Replace(base, `["docker", "-H", "{env}", "ps"]`, `["{env}", "ps", "x"]`, 1),
		"remote on a non-docker": strings.Replace(base, `["docker", "-H", "{env}", "ps"]`, `["curl", "{env}"]`, 1),
		"network unknown key":    strings.Replace(base, `"userHosts": true`, `"userHosts": true, "any": 1`, 1),
		"network bad host":       strings.Replace(base, `"api.example.org"`, `"https://api.example.org"`, 1),
		"network a number":       strings.Replace(base, `{"hosts": ["api.example.org"], "userHosts": true}`, `3`, 1),
	}
	for what, doc := range bad {
		if _, err := ParseManifest([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", what)
		}
	}
}

func TestEnvCheckFiles(t *testing.T) {
	envSetup(t)
	check := func(method, kind, plugin string) error {
		b, _ := json.Marshal(map[string]any{"method": method, "kind": kind, "params": map[string]string{"plugin": plugin}})
		_, err := envCheck(context.Background(), &rpc.Call{Params: b})
		return err
	}
	for m := range fileMethods {
		if err := check(m, "ervisio", "envp"); err != nil {
			t.Errorf("%s on a paired server: %v", m, err)
		}
		for _, k := range []string{"tcp-tls", "ssh", "portainer-agent"} {
			err := check(m, k, "envp")
			if !rpc.IsCode(err, rpc.Invalid) || !strings.Contains(err.Error(), "Files are local") {
				t.Errorf("%s on %s: %v", m, k, err)
			}
		}
		if check(m, "ervisio", "nope") == nil {
			t.Errorf("%s: unknown plugin allowed", m)
		}
	}
}

func TestRemoteArgv(t *testing.T) {
	m, _ := ParseManifest([]byte(strings.ReplaceAll(envManifest, "%SOCK%", "/run/x.sock")))
	cmd := &m.Capabilities.Commands[0]
	got := remoteArgv(cmd, append([]string{}, cmd.Argv...), "")
	if got[2] != "unix:///var/run/docker.sock" {
		t.Fatalf("local: %v", got)
	}
	got = remoteArgv(cmd, append([]string{}, cmd.Argv...), "/run/ervisio/tunnels/1000/env-aabbccdd.sock")
	if got[2] != "unix:///run/ervisio/tunnels/1000/env-aabbccdd.sock" {
		t.Fatalf("remote: %v", got)
	}
	// A command that is not remote is untouched; an argument can never become the endpoint.
	plain := &Command{Name: "x", Argv: []string{"docker", "run", "{0}"}}
	out := remoteArgv(plain, []string{"docker", "run", "{env}"}, "/s")
	if out[2] != "{env}" {
		t.Fatal("non-remote command rewritten")
	}
	out = remoteArgv(cmd, []string{"echo", "-H", "{env}", "{env}"}, "/s")
	if out[3] != "{env}" {
		t.Fatalf("a value equal to the placeholder was replaced: %v", out)
	}
}

func TestEnvSocketOK(t *testing.T) {
	dir, err := os.MkdirTemp(shortTempBase(), "ervs")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := envSocketOK(sock); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(dir, "file")
	os.WriteFile(reg, nil, 0o600)
	for _, bad := range []string{"", "rel.sock", dir + "/../" + filepath.Base(dir) + "/a.sock", reg, filepath.Join(dir, "none.sock"), "/" + strings.Repeat("a", 120)} {
		if envSocketOK(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func envSetup(t *testing.T) string {
	system, _ := setup(t)
	HostsPath = filepath.Join(t.TempDir(), "hosts.json")
	dir, _ := os.MkdirTemp(shortTempBase(), "ervs")
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "api.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"via":"`+r.URL.Path+`"}`) })}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	writePlugin(t, filepath.Join(system, "envp"), strings.ReplaceAll(envManifest, "%SOCK%", jsonPath(filepath.Join(dir, "none.sock"))), map[string]string{"index.js": "x"})
	return sock
}

func TestEnvCheckAndSocketOverride(t *testing.T) {
	tunnel := envSetup(t)
	ctx := context.Background()
	check := func(kind, params string) error {
		b, _ := json.Marshal(map[string]any{"method": "plugins.http", "kind": kind, "params": json.RawMessage(params)})
		_, err := envCheck(ctx, &rpc.Call{Params: b})
		return err
	}
	if err := check("ssh", `{"plugin":"envp","name":"docker"}`); err != nil {
		t.Fatal(err)
	}
	if err := check("ervisio", `{"plugin":"envp","command":"ps"}`); err != nil {
		t.Fatal(err)
	}
	if err := check("ssh", `{"plugin":"envp","req":{"name":"docker"}}`); err != nil {
		t.Fatal("nested request name:", err)
	}
	for what, p := range map[string]string{
		"api without remote":     `{"plugin":"envp","name":"other"}`,
		"command without remote": `{"plugin":"envp","command":"local"}`,
		"unknown plugin":         `{"plugin":"nope","name":"docker"}`,
		"nothing named":          `{"plugin":"envp"}`,
	} {
		if check("ssh", p) == nil {
			t.Errorf("%s: allowed", what)
		}
	}
	if check("floppy", `{"plugin":"envp","name":"docker"}`) == nil {
		t.Error("an unknown kind must not serve the docker family")
	}

	// The API's own socket does not exist; with the tunnel the call works,
	// and an admin-level API (root only) is reachable by the user.
	p := HTTPParams{Plugin: "envp", Name: "docker", Method: "GET", Path: "/version"}
	if _, err := runHTTP(ctx, &rpc.Call{}, p); err == nil {
		t.Fatal("without an environment the dead socket must fail")
	}
	p.EnvSocket = tunnel
	res, err := runHTTP(ctx, &rpc.Call{}, p)
	if err != nil || res.Status != 200 || !strings.Contains(res.Body, "/version") {
		t.Fatalf("through the tunnel: %+v %v", res, err)
	}
	// The root bridge never takes an environment socket; an API that does not opt in refuses it.
	if _, err := runHTTP(ctx, &rpc.Call{Admin: true}, p); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("admin call with a tunnel: %v", err)
	}
	p.Name = "other"
	if _, err := runHTTP(ctx, &rpc.Call{}, p); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("api without remote: %v", err)
	}
	// A socket that is not the user's tunnel (a regular file) is refused.
	p.Name, p.EnvSocket = "docker", filepath.Join(filepath.Dir(tunnel), "x")
	os.WriteFile(p.EnvSocket, nil, 0o600)
	if _, err := runHTTP(ctx, &rpc.Call{}, p); err == nil {
		t.Fatal("a regular file accepted as a tunnel")
	}

	// Commands: the {env} item gets the tunnel.
	r, err := resolve(&rpc.Call{}, ExecParams{Plugin: "envp", Command: "ps", EnvSocket: tunnel}, false)
	if err != nil || r.argv[2] != "unix://"+tunnel {
		t.Fatalf("remote command: %v %v", r, err)
	}
	r, err = resolve(&rpc.Call{}, ExecParams{Plugin: "envp", Command: "ps"}, false)
	if err != nil || r.argv[2] != "unix:///var/run/docker.sock" {
		t.Fatalf("local command: %v %v", r, err)
	}
	if _, err := resolve(&rpc.Call{}, ExecParams{Plugin: "envp", Command: "local", EnvSocket: tunnel}, false); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("command without remote on an environment: %v", err)
	}
}

func TestNormaliseHost(t *testing.T) {
	for in, want := range map[string]string{
		"Registry.Example.org":      "registry.example.org:443",
		"registry.example.org:5000": "registry.example.org:5000",
		"  hub.docker.com ":         "hub.docker.com:443",
	} {
		if got, err := NormaliseHost(in, "https"); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if got, _ := NormaliseHost("intranet", "http"); got != "intranet:80" {
		t.Errorf("http default port: %q", got)
	}
	for _, bad := range []string{"", "*.example.org", "https://a.example.org", "a.example.org/path", "u@a.example.org", "a.example.org:0", "a.example.org:99999", "a b", "-x.org"} {
		if _, err := NormaliseHost(bad, "https"); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestUserHostApproval(t *testing.T) {
	envSetup(t)
	ctx := context.Background()
	call := func(method string, p any, admin bool) (any, error) {
		b, _ := json.Marshal(p)
		fn := map[string]func(context.Context, *rpc.Call) (any, error){
			"plugins.network.request": netRequest, "plugins.network.approve": netApprove,
			"plugins.network.list": netList, "plugins.network.revoke": netRevoke}[method]
		return fn(ctx, &rpc.Call{Params: b, Admin: admin})
	}
	req := func(host, scheme string) NetworkRequest {
		res, err := call("plugins.network.request", map[string]string{"plugin": "envp", "host": host, "scheme": scheme}, false)
		if err != nil {
			t.Fatal(err)
		}
		return res.(NetworkRequest)
	}
	if r := req("api.example.org", ""); r.Status != "approved" {
		t.Fatalf("declared host: %+v", r)
	}
	if r := req("registry.lan:5000", ""); r.Status != "pending" || r.Host != "registry.lan:5000" {
		t.Fatalf("new host: %+v", r)
	}
	if _, err := call("plugins.network.approve", map[string]string{"plugin": "envp", "host": "registry.lan:5000"}, true); err != nil {
		t.Fatal(err)
	}
	if r := req("registry.lan:5000", ""); r.Status != "approved" || r.Scheme != "https" {
		t.Fatalf("after approval: %+v", r)
	}
	// Approved for https only: http is still pending, and another port too.
	if r := req("registry.lan:5000", "http"); r.Status != "pending" {
		t.Fatalf("http after an https approval: %+v", r)
	}
	if r := req("registry.lan:5001", ""); r.Status != "pending" {
		t.Fatalf("other port: %+v", r)
	}
	if _, err := call("plugins.network.approve", map[string]string{"plugin": "envp", "host": "plain.lan", "scheme": "http"}, true); err != nil {
		t.Fatal(err)
	}
	// An http approval is not one for https (review: the frame's policy
	// would not allow it anyway).
	if r := req("plain.lan", "http"); r.Status != "approved" || r.Scheme != "http" {
		t.Fatalf("http after an http approval: %+v", r)
	}
	if r := req("plain.lan", "https"); r.Status != "pending" {
		t.Fatalf("https after an http approval: %+v", r)
	}
	// The frame's CSP list gets both, http ones with their scheme.
	info, err := access(ctx, &rpc.Call{Params: []byte(`{"id":"envp"}`)})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(info.(AccessInfo).Network, " ")
	if !strings.Contains(got, "api.example.org") || !strings.Contains(got, "registry.lan:5000") || !strings.Contains(got, "http://plain.lan:80") {
		t.Fatalf("access network: %s", got)
	}
	if _, err := call("plugins.network.revoke", map[string]string{"plugin": "envp", "host": "registry.lan:5000"}, true); err != nil {
		t.Fatal(err)
	}
	if r := req("registry.lan:5000", ""); r.Status != "pending" {
		t.Fatalf("after revoke: %+v", r)
	}
	// A plugin that did not declare userHosts cannot ask.
	system, _ := setup(t)
	HostsPath = filepath.Join(t.TempDir(), "hosts.json")
	writePlugin(t, filepath.Join(system, "demo"), goodManifest, map[string]string{"index.js": "x"})
	b, _ := json.Marshal(map[string]string{"plugin": "demo", "host": "x.example.org"})
	if _, err := netRequest(ctx, &rpc.Call{Params: b}); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("plugin without userHosts: %v", err)
	}
}

// An upload to an environment goes to the tunnel, like every other call (it once used the API's own socket).
func TestUploadUsesTheEnvironmentTunnel(t *testing.T) {
	tunnel := envSetup(t)
	last, err := upload(t, UploadParams{HTTPParams: HTTPParams{Plugin: "envp", Name: "docker", Method: "POST", Path: "/version", EnvSocket: tunnel}, Size: 3}, []byte("abc"), 10)
	if err != nil || last["status"] != float64(200) || !strings.Contains(last["body"].(string), "/version") {
		t.Fatalf("upload through the tunnel: %v %v", last, err)
	}

}

func TestDevFileOverrideKeepsLoadDevOutOfTheHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	DevFile = filepath.Join(t.TempDir(), "state", "plugins-dev.json")
	t.Cleanup(func() { DevFile = "" })
	if err := saveDev(devState{Folders: []string{"/x/y"}}); err != nil {
		t.Fatal(err)
	}
	if got := registeredDev(); len(got) != 1 || got[0] != "/x/y" {
		t.Fatalf("%v", got)
	}
	if _, err := os.Stat(DevFile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); err == nil {
		t.Fatal("the home folder was touched")
	}
}
