package users

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

var keyTypes = map[string]bool{
	"ssh-rsa":                            true,
	"ssh-dss":                            true,
	"ssh-ed25519":                        true,
	"ecdsa-sha2-nistp256":                true,
	"ecdsa-sha2-nistp384":                true,
	"ecdsa-sha2-nistp521":                true,
	"sk-ssh-ed25519@openssh.com":         true,
	"sk-ecdsa-sha2-nistp256@openssh.com": true,
}

const (
	maxKeyLine = 16 << 10
	maxKeyFile = 1 << 20
)

// authKey is one parsed public key line.
type authKey struct {
	Type        string `json:"type"`
	Comment     string `json:"comment"`
	Fingerprint string `json:"fingerprint"`
	Options     string `json:"options"`
	Key         string `json:"key"`  // "<type> <base64>" without options and comment
	Line        int    `json:"line"` // 1-based line number in the file
}

// splitOptions returns the leading options field (may be empty) and the rest.
// Options end at the first whitespace outside double quotes.
func splitOptions(line string) (opts, rest string) {
	inQ := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && inQ && i+1 < len(line):
			i++
		case c == '"':
			inQ = !inQ
		case (c == ' ' || c == '\t') && !inQ:
			return line[:i], strings.TrimLeft(line[i:], " \t")
		}
	}
	return line, ""
}

// parseKeyLine parses one authorized_keys line. ok=false for blank lines and
// comments; err describes a malformed key.
func parseKeyLine(line string) (k authKey, ok bool, err error) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return k, false, nil
	}
	if len(line) > maxKeyLine {
		return k, true, errors.New("the key line is too long")
	}
	rest := line
	first := strings.Fields(line)[0]
	if !keyTypes[first] {
		k.Options, rest = splitOptions(line)
		if rest == "" {
			return k, true, errors.New("no key found after the options")
		}
	}
	f := strings.Fields(rest)
	if len(f) < 2 {
		return k, true, errors.New("a public key looks like \"ssh-ed25519 AAAA… comment\"")
	}
	if !keyTypes[f[0]] {
		return k, true, fmt.Errorf("unsupported key type %q", f[0])
	}
	blob, derr := base64.StdEncoding.DecodeString(f[1])
	if derr != nil || len(blob) < 8 {
		return k, true, errors.New("the key data is not valid base64")
	}
	n := binary.BigEndian.Uint32(blob[:4])
	if int(n) > len(blob)-4 || string(blob[4:4+n]) != f[0] {
		return k, true, errors.New("the key data does not match its type")
	}
	sum := sha256.Sum256(blob)
	k.Type = f[0]
	k.Fingerprint = "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	k.Key = f[0] + " " + f[1]
	if len(f) > 2 {
		// The comment is everything after the key data, spacing preserved.
		i := strings.Index(rest, f[1]) + len(f[1])
		k.Comment = strings.TrimSpace(rest[i:])
	}
	return k, true, nil
}

// parseAuthorizedKeys returns the valid keys of a file's content. Malformed
// lines are skipped (sshd ignores them too).
func parseAuthorizedKeys(data string) []authKey {
	out := []authKey{}
	for i, l := range strings.Split(data, "\n") {
		k, ok, err := parseKeyLine(strings.TrimRight(l, "\r"))
		if ok && err == nil {
			k.Line = i + 1
			out = append(out, k)
		}
	}
	return out
}

// removeKey drops every line holding the key with the fingerprint, keeping the
// rest of the file byte for byte. It returns the new content and the count.
func removeKey(data, fp string) (string, int) {
	lines := strings.Split(data, "\n")
	out := make([]string, 0, len(lines))
	n := 0
	for _, l := range lines {
		if k, ok, err := parseKeyLine(strings.TrimRight(l, "\r")); ok && err == nil && k.Fingerprint == fp {
			n++
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n"), n
}

// appendKey adds a line, making sure the previous content ends in a newline.
func appendKey(data, line string) string {
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	return data + line + "\n"
}

// ---- file access, as the owner ----

// asUser runs fn with the file-system identity of uid/gid when the bridge runs
// as root, so a user-controlled symlink can never make us touch anything the
// user could not. As a normal user it just runs fn.
func asUser(uid, gid int, fn func() error) error {
	if os.Geteuid() != 0 || uid == 0 {
		return fn()
	}
	runtime.LockOSThread()
	if err := syscall.Setfsgid(gid); err != nil {
		runtime.UnlockOSThread()
		return err
	}
	if err := syscall.Setfsuid(uid); err != nil {
		_ = syscall.Setfsgid(0)
		runtime.UnlockOSThread()
		return err
	}
	err := fn()
	e1 := syscall.Setfsuid(0)
	e2 := syscall.Setfsgid(0)
	if e1 == nil && e2 == nil {
		runtime.UnlockOSThread()
	} // else: leave the thread locked so it ends with this goroutine
	return err
}

func readKeyFile(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxKeyFile+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxKeyFile {
		return "", errors.New("authorized_keys is larger than 1 MiB")
	}
	return string(b), nil
}

// writeKeyFile replaces path atomically with mode 0600, creating the 0700 .ssh
// directory when missing. It keeps the owner (the process identity at this
// point, see asUser).
func writeKeyFile(path, content string) error {
	dir := filepath.Dir(path)
	if fi, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	tmp, err := os.OpenFile(filepath.Join(dir, ".authorized_keys.tmp"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(name)
		}
	}()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}
