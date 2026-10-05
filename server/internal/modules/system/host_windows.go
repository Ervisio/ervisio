//go:build windows

package system

// uname is not available on windows yet: the kernel version is left out of the
// host info and the architecture stays the one of the build.
func uname() (kernel, arch string, ok bool) { return "", "", false }
