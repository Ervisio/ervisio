package account

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// ShadowFile is the shadow password file (variable for tests).
var ShadowFile = "/etc/shadow"

// ErrNoShadowEntry means the shadow file was read but has no line for the
// user (accounts from LDAP/sssd, systemd-homed…).
var ErrNoShadowEntry = errors.New("no shadow entry")

// ShadowEntry is the part of a shadow(5) line the daemon needs to tell
// whether an account changed since sign-in. The password hash itself is
// never kept: only its SHA-256 fingerprint.
type ShadowEntry struct {
	// Fingerprint is hex(sha256(password field)): it changes when the
	// password is changed or the account is locked (usermod -L / passwd -l).
	Fingerprint string
	Locked      bool
	// LastChange and Expire are days since the epoch, -1 when empty.
	LastChange int64
	Expire     int64
}

// Expired reports whether the account expiry date has passed.
func (e *ShadowEntry) Expired(now time.Time) bool {
	return e.Expire >= 0 && now.Unix()/86400 >= e.Expire
}

// ReadShadow returns name's shadow entry. Reading /etc/shadow needs root;
// other callers get a permission error.
func ReadShadow(name string) (*ShadowEntry, error) {
	f, err := os.Open(ShadowFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, name+":") {
			continue
		}
		fl := strings.Split(line, ":")
		if len(fl) < 8 || fl[0] != name {
			continue
		}
		sum := sha256.Sum256([]byte(fl[1]))
		return &ShadowEntry{
			Fingerprint: hex.EncodeToString(sum[:]),
			Locked:      strings.HasPrefix(fl[1], "!"),
			LastChange:  shadowDays(fl[2]),
			Expire:      shadowDays(fl[7]),
		}, nil
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNoShadowEntry
}

func shadowDays(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return -1
	}
	return n
}
