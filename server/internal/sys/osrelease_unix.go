//go:build unix

package sys

import (
	"io"
	"os"
	"strings"
)

// ReadOSRelease reads /etc/os-release, falling back to /usr/lib/os-release.
// Missing files yield a generic "linux" release.
func ReadOSRelease() OSRelease {
	for _, p := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()
		return ParseOSRelease(io.LimitReader(f, 64<<10))
	}
	return ParseOSRelease(strings.NewReader(""))
}
