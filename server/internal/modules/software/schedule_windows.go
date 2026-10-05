//go:build windows

package software

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func schtasksPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "schtasks.exe")
}

// schtasks runs schtasks.exe (absolute path: sys.LookPath only knows Unix
// directories) and returns its decoded stdout.
func schtasks(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, schtasksPath(), args...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	out := decodeText(so.Bytes())
	if err != nil {
		msg := strings.TrimSpace(decodeText(se.Bytes()) + " " + out)
		if isAccessDenied(msg) {
			return out, rpc.Errorf(rpc.NeedsAdmin, "Changing the scheduled update needs administrator rights.")
		}
		return out, rpc.Errorf(rpc.Internal, "schtasks: %s", msg)
	}
	return out, nil
}

// currentScheduleOS reads the time back from the task definition.
func currentScheduleOS() string {
	out, err := schtasks(context.Background(), "/Query", "/TN", taskName, "/XML")
	if err != nil {
		return ""
	}
	return parseTaskTime(out)
}

func removeScheduleOS(ctx context.Context) error {
	if currentScheduleOS() == "" {
		return nil
	}
	_, err := schtasks(ctx, "/Delete", "/TN", taskName, "/F")
	return err
}

// wingetExe finds winget.exe. SYSTEM has no per-user app execution alias, so
// the real binary under Program Files\WindowsApps is preferred.
func wingetExe() (string, error) {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	m, _ := filepath.Glob(filepath.Join(pf, "WindowsApps", "Microsoft.DesktopAppInstaller_*", "winget.exe"))
	if len(m) > 0 {
		return m[len(m)-1], nil
	}
	p, err := exec.LookPath("winget")
	if err != nil {
		return "", rpc.Errorf(rpc.Unavailable, "winget is not installed")
	}
	return p, nil
}

func setScheduleOS(ctx context.Context, plans []Plan, at string) (string, error) {
	var acts []taskAction
	for _, p := range plans {
		for _, s := range p.Steps {
			path := s.Name
			if s.Name == "winget" {
				var err error
				if path, err = wingetExe(); err != nil {
					return "", err
				}
			} else if p, err := exec.LookPath(s.Name); err == nil {
				path = p
			}
			acts = append(acts, taskAction{Path: path, Args: s.Args})
		}
	}
	if len(acts) == 0 {
		return "", rpc.Errorf(rpc.Unavailable, "Nothing to schedule.")
	}
	f, err := os.CreateTemp("", "ervisio-task-*.xml")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, werr := f.Write(encodeUTF16(renderTaskXML(at, acts)))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", werr
	}
	if _, err := schtasks(ctx, "/Create", "/TN", taskName, "/XML", f.Name(), "/F"); err != nil {
		return "", err
	}
	return at, nil
}

// osUpdatesCount counts pending Windows Update items (read-only, via the
// Windows Update Agent COM API); -1 when it cannot be determined.
func osUpdatesCount(ctx context.Context) int {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ps := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	script := `$ErrorActionPreference='Stop';(New-Object -ComObject Microsoft.Update.Session).CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0 and Type='Software'").Updates.Count`
	out, err := exec.CommandContext(ctx, ps, "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return -1
	}
	return n
}
