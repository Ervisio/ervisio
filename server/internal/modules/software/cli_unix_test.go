//go:build !windows

package software

import (
	"context"
	"github.com/ervisio/ervisio/server/internal/sys"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeNpmLauncherRunsLiteralAccountPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "account O'Brien $(touch BAD); path")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(root, "node-stage")
	target := filepath.Join(root, "node")
	bin := filepath.Join(stage, "bin")
	npm := filepath.Join(stage, "lib", "node_modules", "npm", "bin")
	for _, dir := range []string{bin, npm} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	node := filepath.Join(bin, "node")
	if err := os.WriteFile(node, []byte("#!/bin/sh\n[ \"$2\" = \"--version\" ] || exit 3\nprintf '11.20.0\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"npm", "npx"} {
		if err := os.WriteFile(filepath.Join(npm, name+"-cli.js"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := prepareNodeLaunchers(context.Background(), stage, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stage, target); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"npm", "npx"} {
		out, err := (sys.Cmd{Name: filepath.Join(target, "bin", name), Args: []string{"--version"}, Dir: root, MaxOutput: 1024}).Output(context.Background())
		if err != nil || strings.TrimSpace(string(out)) != "11.20.0" {
			t.Fatalf("%s launcher did not preserve argv: %s %v", name, out, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "BAD")); !os.IsNotExist(err) {
		t.Fatal("account path executed a shell substitution")
	}
}

func TestCLIPostAdoptionFailureRestoresPrevious(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "codex")
	stage := filepath.Join(root, "new")
	for _, dir := range []string{target, stage} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, "original"), []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := adoptCLI(stage, target); err != nil {
		t.Fatal(err)
	}
	if err := rollbackCLI(target); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "original")); err != nil || string(got) != "previous" {
		t.Fatalf("previous runtime lost: %s %v", got, err)
	}
}

func TestCLIPrivatePathsLocksAndLauncherCollision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root, err := cliRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lockCLI(root)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := lockCLI(root); err == nil {
		again()
		t.Fatal("concurrent account installation accepted")
	}
	unlock()
	unlock, err = lockCLI(root)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	s, _ := cliByID("node-lts")
	node, _ := cliNode(root)
	if err := os.MkdirAll(filepath.Dir(node), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(node, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node", ".ervisio-version"), []byte("v26.13.0"), 0600); err != nil {
		t.Fatal(err)
	}
	if dirs := ManagedCLIDirs(home); len(dirs) != 1 || dirs[0] != filepath.Dir(node) {
		t.Fatalf("managed PATH: %v", dirs)
	}
	bin := filepath.Join(home, ".local", "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(bin, "node")
	if err := os.WriteFile(existing, []byte("unmanaged"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := ensureCLILauncher(root, s); err == nil {
		t.Fatal("unmanaged launcher collision silently accepted")
	}
	got, _ := os.ReadFile(existing)
	if string(got) != "unmanaged" {
		t.Fatal("existing launcher was overwritten")
	}
	if cliLauncherWarning(root, s) == "" {
		t.Fatal("launcher collision hidden")
	}
	if err := os.Remove(existing); err != nil {
		t.Fatal(err)
	}
	if err := ensureCLILauncher(root, s); err != nil {
		t.Fatal(err)
	}
	if err := ensureCLILauncher(root, s); err != nil {
		t.Fatal(err)
	}
	removeCLILauncher(root, s)
	if _, err := os.Lstat(existing); !os.IsNotExist(err) {
		t.Fatal("managed launcher was not removed")
	}
	if err := os.Chmod(filepath.Join(home, ".local"), 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := cliRoot(false); err == nil {
		t.Fatal("other-writable CLI ancestor accepted")
	}
}

func TestCLIManagedEntryStaysInsidePackage(t *testing.T) {
	pkg := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version":"1","bin":{"codex":"../../outside.js"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if entry, _ := cliPackageInfo(pkg, "codex"); entry != "" {
		t.Fatal("escaping executable accepted")
	}
	if err := os.WriteFile(filepath.Join(pkg, "index.js"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version":"1.2.3","bin":{"codex":"index.js"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if entry, version := cliPackageInfo(pkg, "codex"); entry != filepath.Join(pkg, "index.js") || version != "1.2.3" {
		t.Fatal("installed entry not resolved", entry, version)
	}
}
