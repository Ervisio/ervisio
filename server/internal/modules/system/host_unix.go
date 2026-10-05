//go:build unix

package system

import "syscall"

// uname returns the kernel release and the machine architecture.
func uname() (kernel, arch string, ok bool) {
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil {
		return "", "", false
	}
	return utsString(u.Release[:]), utsString(u.Machine[:]), true
}

func utsString(b []int8) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		out = append(out, byte(c))
	}
	return string(out)
}
