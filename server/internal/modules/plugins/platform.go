package plugins

import (
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

// Platforms: the operating systems a plugin runs on, "linux" and/or
// "windows" (manifest and catalog field "platforms"). A plugin that does not
// say is a Linux plugin: every plugin written before Windows support was.
// An incompatible plugin is shown with its platforms but cannot be
// installed, enabled or run, like one that needs a newer core.

var knownPlatforms = map[string]bool{"linux": true, "windows": true}

// effectivePlatforms is the declared list, or ["linux"].
func effectivePlatforms(declared []string) []string {
	if len(declared) == 0 {
		return []string{"linux"}
	}
	return declared
}

func validatePlatforms(p []string) error {
	seen := map[string]bool{}
	for _, s := range p {
		if !knownPlatforms[s] {
			return fmt.Errorf("platforms: %q is not linux or windows", s)
		}
		if seen[s] {
			return fmt.Errorf("platforms: %q is listed twice", s)
		}
		seen[s] = true
	}
	return nil
}

// hostPlatform is "linux" or "windows" (other unix systems count as linux).
func hostPlatform() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	return "linux"
}

// platformProblem says why a plugin for these platforms cannot run here, or "".
func platformProblem(name string, declared []string) string {
	for _, p := range effectivePlatforms(declared) {
		if p == hostPlatform() {
			return ""
		}
	}
	if hostPlatform() == "windows" {
		return fmt.Sprintf("%s works only on Linux.", name)
	}
	return fmt.Sprintf("%s works only on Windows.", name)
}

// ---- entries for some systems only ----
//
// capabilities.commands, capabilities.http and the files folders may say
// "platforms" too, so one manifest carries the Linux and the Windows form of
// the same command (two entries with one name, for systems that do not
// overlap). An entry without it applies to all the plugin's platforms. The
// daemon keeps only the entries for the system it runs on (forHost); the
// install consent and update checks compare the whole declaration.

var (
	winDriveRe = regexp.MustCompile(`^[A-Za-z]:\\`)
	pipeRe     = regexp.MustCompile(`^\\\\\.\\pipe\\[A-Za-z0-9._-]{1,200}$`)
)

func hasDotDot(p string) bool {
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '\\' || r == '/' }) {
		if part == ".." || part == "." {
			return true
		}
	}
	return false
}

func isPipe(s string) bool { return strings.HasPrefix(s, `\\`) }

// pipeOnly: the entry runs on Windows alone.
func pipeOnly(on []string) bool { return len(on) == 1 && on[0] == "windows" }

// entryPlatforms checks an entry's list against the plugin's.
func (c *Capabilities) entryPlatforms(p []string) error {
	if err := validatePlatforms(p); err != nil {
		return err
	}
	for _, s := range p {
		if !slices.Contains(c.plats, s) {
			return fmt.Errorf("platforms: %q is not one of the plugin's platforms", s)
		}
	}
	return nil
}

// on is the systems an entry applies to.
func (c *Capabilities) on(p []string) []string {
	if len(p) == 0 {
		if len(c.plats) == 0 {
			return effectivePlatforms(nil)
		}
		return c.plats
	}
	return p
}

// clash records name for the entry's systems and reports an overlap with an
// earlier entry of the same name.
func (c *Capabilities) clash(seen map[string][][]string, name string, p []string) bool {
	on := c.on(p)
	for _, prev := range seen[name] {
		for _, s := range on {
			if slices.Contains(prev, s) {
				return true
			}
		}
	}
	seen[name] = append(seen[name], on)
	return false
}

func forPlatform(p []string, host string) bool { return len(p) == 0 || slices.Contains(p, host) }

// forHost is the capabilities with only the entries for this system.
func (c Capabilities) forHost() Capabilities {
	host := hostPlatform()
	out := c
	out.Commands = []Command{}
	for _, x := range c.Commands {
		if forPlatform(x.Platforms, host) {
			out.Commands = append(out.Commands, x)
		}
	}
	out.HTTP = []HTTPAPI{}
	for _, x := range c.HTTP {
		if forPlatform(x.Platforms, host) {
			out.HTTP = append(out.HTTP, x)
		}
	}
	keep := func(l []Folder) []Folder {
		r := []Folder{}
		for _, f := range l {
			if forPlatform(f.Platforms, host) {
				r = append(r, f)
			}
		}
		return r
	}
	out.Files.Read = keep(c.Files.Read)
	out.Files.Write = keep(c.Files.Write)
	return out
}
