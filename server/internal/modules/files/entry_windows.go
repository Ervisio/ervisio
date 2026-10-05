//go:build windows

package files

import (
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

// setOwner leaves the numeric owner fields at zero: Windows has no uid/gid.
func setOwner(e *Entry, fi os.FileInfo) {}

var (
	sidMu    sync.Mutex
	sidNames = map[string]string{}
)

// setOwnerPath fills Owner with DOMAIN\name from the file's security
// descriptor. Best effort: any failure leaves it empty. Links are skipped so
// the lookup never follows them.
func setOwnerPath(e *Entry, full string, fi os.FileInfo) {
	if isLinkMode(fi.Mode()) {
		return
	}
	sd, err := windows.GetNamedSecurityInfo(full, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return
	}
	sid, _, err := sd.Owner()
	if err != nil || sid == nil {
		return
	}
	key := sid.String()
	sidMu.Lock()
	name, ok := sidNames[key]
	sidMu.Unlock()
	if !ok {
		name = key
		if acc, dom, _, err := sid.LookupAccount(""); err == nil {
			name = acc
			if dom != "" {
				name = dom + `\` + acc
			}
		}
		sidMu.Lock()
		sidNames[key] = name
		sidMu.Unlock()
	}
	e.Owner = name
}
