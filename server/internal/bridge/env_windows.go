//go:build windows

package bridge

import (
	"os"
	"path/filepath"
	"strings"
)

// env is the Windows environment of the bridge: the system variables the
// daemon itself runs with (they are machine-wide) plus the per-user ones
// derived from the account. The daemon's own (SYSTEM) profile never leaks in.
func (s *Spec) env() []string {
	a := s.Account
	sysRoot := firstNonEmpty(os.Getenv("SYSTEMROOT"), os.Getenv("WINDIR"), `C:\Windows`)
	home := a.Home
	user, domain := a.Name, os.Getenv("COMPUTERNAME")
	if i := strings.IndexByte(user, '\\'); i >= 0 {
		domain, user = user[:i], user[i+1:]
	}
	env := []string{
		"SYSTEMROOT=" + sysRoot,
		"WINDIR=" + sysRoot,
		"SYSTEMDRIVE=" + firstNonEmpty(os.Getenv("SYSTEMDRIVE"), filepath.VolumeName(sysRoot)),
		"USERNAME=" + user,
		"USERDOMAIN=" + domain,
		"COMSPEC=" + firstNonEmpty(os.Getenv("COMSPEC"), filepath.Join(sysRoot, "System32", "cmd.exe")),
		"PATHEXT=" + firstNonEmpty(os.Getenv("PATHEXT"), ".COM;.EXE;.BAT;.CMD"),
		"PATH=" + firstNonEmpty(os.Getenv("PATH"), filepath.Join(sysRoot, "System32")+";"+sysRoot),
		"PROGRAMDATA=" + firstNonEmpty(os.Getenv("PROGRAMDATA"), `C:\ProgramData`),
		"PROGRAMFILES=" + firstNonEmpty(os.Getenv("PROGRAMFILES"), `C:\Program Files`),
	}
	if v := os.Getenv("PROGRAMFILES(X86)"); v != "" {
		env = append(env, "PROGRAMFILES(X86)="+v)
	}
	if home != "" {
		local := filepath.Join(home, "AppData", "Local")
		tmp := filepath.Join(local, "Temp")
		env = append(env,
			"USERPROFILE="+home,
			"HOMEDRIVE="+filepath.VolumeName(home),
			"HOMEPATH="+strings.TrimPrefix(home, filepath.VolumeName(home)),
			"APPDATA="+filepath.Join(home, "AppData", "Roaming"),
			"LOCALAPPDATA="+local,
			"TEMP="+tmp,
			"TMP="+tmp,
		)
	}
	return env
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
