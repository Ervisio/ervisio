package sshauth

import (
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Authorize reports whether pub may sign u in from ip: it reads u's
// authorized_keys files (see ReadAuthorizedKeys) and applies FindKey.
// The returned error wraps ErrNotAuthorized and says why, for the log only
// (files skipped for unsafe permissions are named).
func Authorize(u User, pub ssh.PublicKey, ip net.IP, now time.Time) (*MatchResult, error) {
	data, problems := ReadAuthorizedKeys(u)
	m, err := FindKey(data, pub, ip, now)
	if err != nil && len(problems) > 0 {
		return nil, fmt.Errorf("%w (skipped: %s)", err, strings.Join(problems, "; "))
	}
	return m, err
}
