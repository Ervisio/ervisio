package files

// Pure helpers of the Windows port. They contain no Windows calls so they are
// built (and tested) on every platform.

import (
	"os"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// reparseTagNameSurrogate is the bit that marks symlinks, junctions and other
// reparse points that redirect to another path (IsReparseTagNameSurrogate).
const reparseTagNameSurrogate = 0x20000000

func isNameSurrogate(tag uint32) bool { return tag&reparseTagNameSurrogate != 0 }

// isLinkMode reports whether a Lstat mode describes a symlink or junction. Go
// reports name-surrogate reparse points as ModeSymlink or ModeIrregular;
// placeholders such as OneDrive files stay regular and are not links.
func isLinkMode(m os.FileMode) bool { return m&(os.ModeSymlink|os.ModeIrregular) != 0 }

// winNorm turns / into \ and drops trailing separators (except on a drive root).
func winNorm(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	for len(p) > 3 && strings.HasSuffix(p, `\`) {
		p = p[:len(p)-1]
	}
	return p
}

// isInsideWin is isInside for Windows paths: case-insensitive, \ or / separated.
func isInsideWin(child, parent string) bool {
	c, p := winNorm(child), winNorm(parent)
	if strings.EqualFold(c, p) {
		return true
	}
	if !strings.HasSuffix(p, `\`) {
		p += `\`
	}
	return len(c) >= len(p) && strings.EqualFold(c[:len(p)], p)
}

// windowsProtectedPath reports whether p is a drive root or a system folder
// that must never be deleted or moved. extra holds environment-derived paths
// (SystemRoot, ProgramFiles, ...).
func windowsProtectedPath(p string, extra []string) bool {
	n := winNorm(p)
	if len(n) == 2 && n[1] == ':' || len(n) == 3 && n[1] == ':' && n[2] == '\\' || n == `\` {
		return true
	}
	for _, e := range extra {
		if e != "" && strings.EqualFold(n, winNorm(e)) {
			return true
		}
	}
	if len(n) > 3 && n[1] == ':' && n[2] == '\\' {
		switch strings.ToLower(n[3:]) {
		case "windows", "users", "program files", "program files (x86)", "programdata",
			"recovery", "system volume information", "$recycle.bin", "boot", "documents and settings":
			return true
		}
	}
	return false
}

// winReadOnlyFromMode maps a Unix-style mode to the only thing Windows files
// share with it: the read-only attribute (owner write bit). Special bits, and
// modes without owner read access, have no Windows meaning and are refused.
func winReadOnlyFromMode(m os.FileMode) (bool, error) {
	if m&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false, rpc.Errorf(rpc.Invalid, "Setuid, setgid and sticky bits do not exist on Windows. Use a mode such as 644 (writable) or 444 (read-only).")
	}
	if m&0o400 == 0 {
		return false, rpc.Errorf(rpc.Invalid, "On Windows only the read-only attribute can be changed: use a mode with owner read access, such as 644 (writable) or 444 (read-only).")
	}
	return m&0o200 == 0, nil
}
