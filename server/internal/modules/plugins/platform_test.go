package plugins

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestPlatforms(t *testing.T) {
	if err := validatePlatforms([]string{"linux", "windows"}); err != nil {
		t.Error(err)
	}
	for _, bad := range [][]string{{"macos"}, {"linux", "linux"}} {
		if validatePlatforms(bad) == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	other := "windows"
	if runtime.GOOS == "windows" {
		other = "linux"
	}
	if platformProblem("P", []string{"linux", "windows"}) != "" || platformProblem("P", []string{hostPlatform()}) != "" {
		t.Error("compatible plugin refused")
	}
	if platformProblem("P", []string{other}) == "" {
		t.Error("plugin for the other OS accepted")
	}
	if got := effectivePlatforms(nil); len(got) != 1 || got[0] != "linux" {
		t.Errorf("default %v", got)
	}
}

func TestEntryPlatforms(t *testing.T) {
	const base = `{"id":"pp","name":"P","version":"1.0.0","entry":"index.js","platforms":["linux","windows"],
	"capabilities":{%s},"contributes":{},"visibleTo":{}}`
	ok := `"commands":[
		{"name":"ls","argv":["ls","-1"],"platforms":["linux"]},
		{"name":"ls","argv":["powershell.exe","-NoProfile","-Command","Get-ChildItem"],"platforms":["windows"]},
		{"name":"both","argv":["hostname"]}],
	"http":[
		{"name":"docker","socket":"/run/docker.sock","platforms":["linux"],"rules":[{"methods":["GET"],"path":"/.*"}]},
		{"name":"docker","socket":"\\\\.\\pipe\\docker_engine","platforms":["windows"],"rules":[{"methods":["GET"],"path":"/.*"}]}],
	"files":{"read":[{"path":"/var/log","platforms":["linux"]},{"path":"C:\\ProgramData\\Demo","platforms":["windows"]}]}`
	m, err := ParseManifest([]byte(fmt.Sprintf(base, ok)))
	if err != nil {
		t.Fatal(err)
	}
	host := hostPlatform()
	if len(m.Capabilities.Commands) != 2 || len(m.Capabilities.HTTP) != 1 || len(m.Capabilities.Files.Read) != 1 {
		t.Fatalf("host %s kept %+v", host, m.Capabilities)
	}
	if len(m.declared.Commands) != 3 || len(m.declared.HTTP) != 2 {
		t.Fatalf("declared lost entries: %+v", m.declared)
	}
	wantArgv0 := map[string]string{"linux": "ls", "windows": "powershell.exe"}[host]
	if c := m.Command("ls"); c == nil || c.Argv[0] != wantArgv0 {
		t.Fatalf("ls on %s: %+v", host, c)
	}
	for name, caps := range map[string]string{
		"same name, same system": `"commands":[{"name":"a","argv":["x"]},{"name":"a","argv":["y"],"platforms":["windows"]}]`,
		"pipe on linux":          `"http":[{"name":"d","socket":"\\\\.\\pipe\\x","platforms":["linux"],"rules":[{"methods":["GET"],"path":"/"}]}]`,
		"pipe for both":          `"http":[{"name":"d","socket":"\\\\.\\pipe\\x","rules":[{"methods":["GET"],"path":"/"}]}]`,
		"drive path on linux":    `"files":{"read":[{"path":"C:\\x","platforms":["linux"]}]}`,
		"unknown system":         `"commands":[{"name":"a","argv":["x"],"platforms":["macos"]}]`,
	} {
		if _, err := ParseManifest([]byte(fmt.Sprintf(base, caps))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// An entry for a system the plugin does not run on is refused.
	if _, err := ParseManifest([]byte(strings.Replace(fmt.Sprintf(base, `"commands":[{"name":"a","argv":["x"],"platforms":["windows"]}]`), `"platforms":["linux","windows"],`, `"platforms":["linux"],`, 1))); err == nil {
		t.Error("windows entry in a linux plugin accepted")
	}
}
