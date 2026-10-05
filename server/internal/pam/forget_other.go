//go:build !windows

package pam

// Forget does nothing: only the Windows backend keeps a logon token per user.
func Forget(user string) {}
