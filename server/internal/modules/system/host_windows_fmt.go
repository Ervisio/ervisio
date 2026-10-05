package system

import "strings"

// windowsKernel formats the NT build as "10.0.<build>[.<ubr>]". The build
// number is the closest thing Windows has to a kernel version.
func windowsKernel(build, ubr string) string {
	build = strings.TrimSpace(build)
	if build == "" {
		return ""
	}
	k := "10.0." + build
	if ubr = strings.TrimSpace(ubr); ubr != "" {
		k += "." + ubr
	}
	return k
}
