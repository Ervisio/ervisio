package update

import (
	"os/exec"
	"strings"
	"testing"
)

// Exercise the real POSIX installer functions without invoking installation,
// networking, service control or filesystem mutations.
func TestInstallScriptUIChoice(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell unavailable")
	}
	source := strings.TrimSuffix(readInstallScript(t), "main \"$@\" </dev/null\n")
	for _, mode := range []string{"classic", "ervisios", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			harness := `
CFG_REQUESTED=0
opt_set --ui '` + mode + `'
[ "$F_UI" = '` + mode + `' ] || exit 3
[ "$CFG_REQUESTED" = 1 ] || exit 4
C_HOST=127.0.0.1 C_PORT=9090 C_ROOT=false C_UNLOCK=5m C_TLS=self-signed
C_UI=$F_UI C_AUSERS='' C_AGROUPS='' C_ADMINS=false C_ORIGINS='' C_PROXIES=''
render_config
`
			cmd := exec.Command(sh, "-s")
			cmd.Stdin = strings.NewReader(source + harness)
			out, err := cmd.CombinedOutput()
			if mode == "invalid" {
				if err == nil || !strings.Contains(string(out), "must be classic or ervisios") {
					t.Fatalf("invalid UI accepted: %v %s", err, out)
				}
			} else if err != nil || !strings.Contains(string(out), "[ui]\n") || !strings.Contains(string(out), `default_shell = "`+mode+`"`) {
				t.Fatalf("UI choice not rendered: %v %s", err, out)
			}
		})
	}
}
