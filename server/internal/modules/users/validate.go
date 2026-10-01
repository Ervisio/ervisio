package users

import (
	"regexp"
	"strings"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// nameRe is the POSIX portable user/group name (lower case, as useradd's
// default), at most 32 characters, not starting with a digit or a dash.
var nameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func validName(kind, n string) error {
	if !nameRe.MatchString(n) {
		return rpc.Errorf(rpc.Invalid, "%s names use lower-case letters, digits, '_' and '-', start with a letter or '_', and have at most 32 characters (got %q)", kind, n)
	}
	return nil
}

// validFullName checks a GECOS "full name" field.
func validFullName(s string) error {
	if len(s) > 128 {
		return rpc.Errorf(rpc.Invalid, "The full name is too long (128 characters at most)")
	}
	for _, r := range s {
		if r == ':' || r == ',' || r == '\\' || r == '=' || r < 0x20 || r == 0x7f {
			return rpc.Errorf(rpc.Invalid, "The full name cannot contain ':', ',', '=', backslashes or control characters")
		}
	}
	return nil
}

// validPassword checks what chpasswd can carry on one line.
func validPassword(p string) error {
	if p == "" {
		return rpc.Errorf(rpc.Invalid, "Enter a password")
	}
	if len(p) > 256 {
		return rpc.Errorf(rpc.Invalid, "The password is too long (256 characters at most)")
	}
	if strings.ContainsAny(p, "\n\r\x00") {
		return rpc.Errorf(rpc.Invalid, "The password cannot contain line breaks")
	}
	return nil
}

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
