//go:build windows

package system

import "github.com/ervisio/ervisio/server/internal/sys"

func platformConnectivity() connectivityDriver {
	return &windowsConnectivity{lookPath: sys.LookPath, run: runRadio}
}
