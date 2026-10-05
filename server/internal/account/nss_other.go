//go:build !cgo || windows

package account

// nssShell has no NSS to ask without cgo or on Windows: the shell is unknown.
func nssShell(name string) string { return "" }
