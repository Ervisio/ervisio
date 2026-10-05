//go:build !windows

package bridge

import (
	"os"
	"strconv"

	"github.com/ervisio/ervisio/server/internal/sys"
)

// env is the Unix environment of the bridge.
func (s *Spec) env() []string {
	a := s.Account
	env := []string{
		"PATH=" + sys.SafePath,
		"HOME=" + a.Home,
		"USER=" + a.Name,
		"LOGNAME=" + a.Name,
		"LANG=C.UTF-8",
	}
	if a.Shell != "" {
		env = append(env, "SHELL="+a.Shell)
	}
	rt := "/run/user/" + strconv.FormatUint(uint64(a.UID), 10)
	if fi, err := os.Stat(rt); err == nil && fi.IsDir() {
		env = append(env, "XDG_RUNTIME_DIR="+rt)
	}
	return env
}
