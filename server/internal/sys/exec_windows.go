//go:build windows

package sys

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// setProcessGroup starts the command in a new process group and kills it
// when its context ends. Child processes are not tracked yet: that needs a
// job object.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil
	}
}

func systemRoot() string {
	if r := os.Getenv("SYSTEMROOT"); r != "" {
		return r
	}
	if r := os.Getenv("WINDIR"); r != "" {
		return r
	}
	return `C:\Windows`
}

// safePath is the fixed command search path on Windows: the system folders
// (never the caller's PATH, never the current folder).
func safePath() string {
	r := systemRoot()
	return strings.Join([]string{
		filepath.Join(r, "System32"),
		r,
		filepath.Join(r, "System32", "Wbem"),
		filepath.Join(r, "System32", "WindowsPowerShell", "v1.0"),
		filepath.Join(r, "System32", "OpenSSH"),
	}, string(os.PathListSeparator))
}

// keptEnv lists variables copied from the bridge environment: the ones
// Windows programs need to start and find their folders.
var keptEnv = []string{
	"SYSTEMROOT", "WINDIR", "SYSTEMDRIVE", "COMSPEC", "PATHEXT", "TEMP", "TMP",
	"USERPROFILE", "USERNAME", "USERDOMAIN", "COMPUTERNAME", "HOMEDRIVE", "HOMEPATH",
	"APPDATA", "LOCALAPPDATA", "PROGRAMDATA", "PROGRAMFILES", "PROGRAMFILES(X86)",
	"PROGRAMW6432", "OS", "PROCESSOR_ARCHITECTURE", "NUMBER_OF_PROCESSORS", "TZ",
}

// pathSeps are the separators that make a command name a path.
const pathSeps = `/\`

// findExecutable returns the program p names: p itself when it has an
// executable extension, else p with .exe or .com added. Windows has no
// execute bit: a regular file with the right extension is a program.
func findExecutable(p string) (string, bool) {
	isFile := func(f string) bool {
		fi, err := os.Stat(f)
		return err == nil && fi.Mode().IsRegular()
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".exe", ".com":
		return p, isFile(p)
	}
	for _, ext := range []string{".exe", ".com"} {
		if isFile(p + ext) {
			return p + ext, true
		}
	}
	return p, false
}

// extraLookup finds programs that are not in the system folders: winget
// lives in the App Installer package (Program Files\WindowsApps, which a
// service can read) or, for a user, as an app-execution alias.
func extraLookup(name string) (string, bool) {
	if !strings.EqualFold(name, "winget") {
		return "", false
	}
	pf := os.Getenv("PROGRAMFILES")
	if pf == "" {
		pf = `C:\Program Files`
	}
	if m, _ := filepath.Glob(filepath.Join(pf, "WindowsApps", "Microsoft.DesktopAppInstaller_*", "winget.exe")); len(m) > 0 {
		return m[len(m)-1], true
	}
	if l := os.Getenv("LOCALAPPDATA"); l != "" {
		p := filepath.Join(l, "Microsoft", "WindowsApps", "winget.exe")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true
		}
	}
	return "", false
}
