package bridge

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/pam"
)

// HelperFlag is the first argument that turns ervisiod into the PAM
// session helper (see RunSessionHelper).
const HelperFlag = "--pam-session-helper"

// helperCommand returns the command that starts the user bridge through the
// session helper. The helper runs as root (no credential switch here) in a
// new session; it drops to the account itself.
func (s *Spec) helperCommand() *exec.Cmd {
	svc := s.PAMService
	if svc == "" {
		svc = pam.Service()
	}
	args := []string{HelperFlag,
		"--user", s.Account.Name,
		"--uid", strconv.FormatUint(uint64(s.Account.UID), 10),
		"--service", svc,
		"--rhost", s.RHost,
		"--", s.Bridge}
	args = append(args, s.bridgeArgs(false)...)
	cmd := exec.Command(s.SessionHelper, args...)
	cmd.Env = s.env()
	cmd.Dir = "/"
	setSessionAttr(cmd)
	return cmd
}

// mergeEnv adds the PAM session environment to base. PAM entries replace
// base entries with the same name, except PATH (the bridge uses a fixed
// one) and the identity variables set by the daemon.
func mergeEnv(base, pamEnv []string) []string {
	keep := map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true}
	out := slices.Clone(base)
	idx := map[string]int{}
	for i, kv := range out {
		if k, _, ok := strings.Cut(kv, "="); ok {
			idx[k] = i
		}
	}
	for _, kv := range pamEnv {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" || keep[k] || strings.HasPrefix(k, "LD_") {
			continue
		}
		if i, ok := idx[k]; ok {
			out[i] = kv
		} else {
			idx[k] = len(out)
			out = append(out, kv)
		}
	}
	return out
}

// RunSessionHelper is the main function of `ervisiod --pam-session-helper`.
// Started as root by the daemon (stdin/stdout are the bridge protocol), it
// opens a PAM session for the account (pam_acct_mgmt, pam_setcred,
// pam_open_session: pam_limits, pam_loginuid, pam_systemd apply), starts
// the bridge with the account's uid/gid/groups inside that session, passes
// its stdio through, forwards termination signals, and closes the PAM
// session when the bridge exits. It returns the exit status.
func RunSessionHelper(args []string) int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(os.Stderr, "pam session: "+format+"\n", a...)
		return 1
	}
	fs := flag.NewFlagSet(HelperFlag, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	user := fs.String("user", "", "")
	uid := fs.Uint64("uid", 0, "")
	service := fs.String("service", "", "")
	rhost := fs.String("rhost", "", "")
	if err := fs.Parse(args); err != nil {
		return fail("%v", err)
	}
	argv := fs.Args()
	if len(argv) == 0 || !filepath.IsAbs(argv[0]) {
		return fail("missing absolute bridge path")
	}
	if os.Geteuid() != 0 {
		return fail("must run as root")
	}
	if *service == "" || strings.ContainsAny(*service, "/\x00") {
		return fail("invalid PAM service")
	}
	a, err := account.Lookup(*user)
	if err != nil {
		return fail("account %q: %v", *user, err)
	}
	if uint64(a.UID) != *uid {
		return fail("account %q now has uid %d, expected %d", a.Name, a.UID, *uid)
	}
	if !account.ShellAllowed(a.Shell) {
		return fail("account %q has no login shell (%q)", a.Name, a.Shell)
	}
	ps, err := pam.OpenSession(*service, a.Name, *rhost)
	if err != nil {
		return fail("open session for %q: %v", a.Name, err)
	}
	defer func() {
		if err := ps.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "pam session: %v\n", err)
		}
	}()

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = mergeEnv(os.Environ(), ps.Env())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Dir = "/"
	if fi, err := os.Stat(a.Home); err == nil && fi.IsDir() {
		cmd.Dir = a.Home
	}
	setUserAttr(cmd, a)
	sigs := make(chan os.Signal, 4)
	notifySignals(sigs)
	if err := cmd.Start(); err != nil {
		return fail("start bridge: %v", err)
	}
	// The bridge owns stdio now; drop our copies so EOF reaches it alone.
	os.Stdin.Close()
	os.Stdout.Close()
	go func() {
		for sig := range sigs {
			_ = cmd.Process.Signal(sig)
		}
	}()
	err = cmd.Wait()
	signal.Stop(sigs)
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ee.ExitCode() >= 0 {
			return ee.ExitCode()
		}
		return 1
	}
	if err != nil {
		return fail("bridge: %v", err)
	}
	return 0
}
