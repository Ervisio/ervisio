package terminal

import (
	"os"
	"strings"

	"github.com/ervisio/ervisio/server/internal/modules/software"
)

func withManagedCLIPath(env []string, home string) []string {
	dirs := software.ManagedCLIDirs(home)
	return prependManagedPath(env, dirs)
}

func prependManagedPath(env, dirs []string) []string {
	if len(dirs) == 0 {
		return env
	}
	out := make([]string, 0, len(env)+1)
	base := ""
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, "PATH") {
			if base == "" {
				base = value
			}
			continue
		}
		out = append(out, entry)
	}
	path := strings.Join(dirs, string(os.PathListSeparator))
	if base != "" {
		path += string(os.PathListSeparator) + base
	}
	return append(out, "PATH="+path)
}
