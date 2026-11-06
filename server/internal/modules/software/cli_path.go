package software

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ManagedCLIDirs is only for interactive terminals of this process's account.
// It never accepts a different account's home or changes the bridge's SafePath.
func ManagedCLIDirs(home string) []string {
	own, err := os.UserHomeDir()
	if err != nil || filepath.Clean(home) != filepath.Clean(own) {
		return nil
	}
	root, err := cliRoot(false)
	if err != nil {
		return nil
	}
	var out []string
	for _, id := range []string{"node", "claude-code", "codex"} {
		base := filepath.Join(root, id)
		if checkTrustedDir(base, os.Geteuid()) != nil {
			continue
		}
		if runtime.GOOS != "windows" {
			base = filepath.Join(base, "bin")
		}
		if checkTrustedDir(base, os.Geteuid()) == nil {
			out = append(out, base)
		}
	}
	return out
}

func cliLauncherPath(root string, s cliSpec) string {
	return filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(root))), "bin", s.Command)
}
func cliLauncherWarning(root string, s cliSpec) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	path := cliLauncherPath(root, s)
	target, err := os.Readlink(path)
	if err == nil && target == filepath.Join(root, "launchers", s.Command) {
		return ""
	}
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return ""
	}
	return "An existing launcher was kept. Use Open terminal to launch this managed copy."
}

func ensureCLILauncher(root string, s cliSpec) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	entry, _ := cliManaged(root, s)
	if s.ID == "node-lts" && s.Command != "node" {
		entry = filepath.Join(root, "node", "bin", s.Command)
	}
	if entry == "" {
		return fmt.Errorf("No CLI executable for launcher")
	}
	bin := filepath.Dir(cliLauncherPath(root, s))
	if err := os.Mkdir(bin, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	if err := checkTrustedDir(bin, os.Geteuid()); err != nil {
		return err
	}
	launchers := filepath.Join(root, "launchers")
	if err := os.Mkdir(launchers, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	if err := checkTrustedDir(launchers, os.Geteuid()); err != nil {
		return err
	}
	command := cliTerminal(root, entry)
	node, _ := cliNode(root)
	content := "#!/bin/sh\nPATH=" + cliQuote(filepath.Dir(node)) + ":\"$PATH\"\nexport PATH\nexec " + command + " \"$@\"\n"
	target := filepath.Join(launchers, s.Command)
	if err := writeFileAtomic(target, []byte(content), 0700); err != nil {
		return err
	}
	path := filepath.Join(bin, s.Command)
	if existing, err := os.Lstat(path); err == nil {
		link, readErr := os.Readlink(path)
		rel, relErr := filepath.Rel(root, link)
		if existing.Mode()&os.ModeSymlink == 0 || ownerOf(existing) != os.Geteuid() || readErr != nil || relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("An existing %s launcher was kept; Open terminal uses the managed executable directly", s.Command)
		}
		if link == target {
			return nil
		}
		return fmt.Errorf("An existing managed launcher points elsewhere; it was preserved")
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, path)
}

func removeCLILauncher(root string, s cliSpec) {
	if runtime.GOOS == "windows" {
		return
	}
	path := cliLauncherPath(root, s)
	if target, err := os.Readlink(path); err == nil && target == filepath.Join(root, "launchers", s.Command) {
		_ = os.Remove(path)
	}
	_ = os.Remove(filepath.Join(root, "launchers", s.Command))
}
