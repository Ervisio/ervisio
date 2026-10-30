package files

import "os"

// Helpers shared with the remote module (modules/remote), which shows
// SFTP, FTP, SMB and WebDAV folders with the same Entry shape and the same
// text handling as local files.

// ModeString renders permission bits like ls does ("rw-r--r--").
func ModeString(m os.FileMode) string { return modeString(m) }

// ModeOctal renders permission bits as four octal digits ("0644").
func ModeOctal(m os.FileMode) string { return modeOctal(m) }

// TypeOf names the kind of a file: file, dir, symlink or other.
func TypeOf(m os.FileMode) string { return typeOf(m) }

// MimeOf guesses a MIME type from a file name ("" when unknown).
func MimeOf(name string) string { return mimeOf(name) }

// DecodeText turns raw bytes into text and names the encoding (utf-8,
// utf-8-bom, latin1). It refuses binary data with an invalid error.
func DecodeText(b []byte, truncated bool) (string, string, error) { return decodeText(b, truncated) }

// EncodeText is the reverse of DecodeText.
func EncodeText(s, enc string) ([]byte, error) { return encodeText(s, enc) }

// CheckNotProtected refuses system folders that may never be moved away.
func CheckNotProtected(p string) error { return checkNotProtected(p) }

// MaxReadText is the largest text read or written in one call.
const MaxReadText = maxReadText
