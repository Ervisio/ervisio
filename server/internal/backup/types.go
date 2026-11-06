// Package backup implements the account-owned, bounded worker used by the
// official Backup plugin. Scheduling remains in Ervisio's existing job manager.
package backup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxInput           = 256 << 10
	maxJobs            = 20
	maxEntries         = 200000
	defaultBytes int64 = 2 << 30
	reserveBytes int64 = 64 << 20
)

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var connectionPattern = regexp.MustCompile(`^[a-f0-9]{16}$`)
var databasePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_. -]{0,127}$`)

type Period struct {
	Value int    `json:"value"`
	Unit  string `json:"unit"`
}

type Destination struct {
	Kind              string  `json:"kind,omitempty"`
	Connection        string  `json:"connection"`
	Path              string  `json:"path"`
	URL               string  `json:"url,omitempty"`
	Method            string  `json:"method,omitempty"`
	UploadMode        string  `json:"uploadMode,omitempty"`
	Field             string  `json:"field,omitempty"`
	APIHeader         string  `json:"apiHeader,omitempty"`
	AuthScheme        string  `json:"authScheme,omitempty"`
	AllowHTTP         bool    `json:"allowHTTP,omitempty"`
	Folder            string  `json:"folder,omitempty"`
	Owner             string  `json:"owner,omitempty"`
	Repository        string  `json:"repository,omitempty"`
	Target            string  `json:"target,omitempty"`
	Bucket            string  `json:"bucket,omitempty"`
	Region            string  `json:"region,omitempty"`
	Endpoint          string  `json:"endpoint,omitempty"`
	PathStyle         bool    `json:"pathStyle,omitempty"`
	Retention         bool    `json:"retention,omitempty"`
	CredentialMode    string  `json:"credentialMode,omitempty"`
	APIKey            *string `json:"apiKey,omitempty"`
	AccessKey         *string `json:"accessKey,omitempty"`
	SecretKey         *string `json:"secretKey,omitempty"`
	SessionToken      *string `json:"sessionToken,omitempty"`
	HasAPIKey         bool    `json:"hasAPIKey,omitempty"`
	HasCredentials    bool    `json:"hasCredentials,omitempty"`
	HasSessionToken   bool    `json:"hasSessionToken,omitempty"`
	ClearAPIKey       bool    `json:"clearAPIKey,omitempty"`
	ClearCredentials  bool    `json:"clearCredentials,omitempty"`
	ClearSessionToken bool    `json:"clearSessionToken,omitempty"`
}

type Database struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	Host          string  `json:"host,omitempty"`
	Port          int     `json:"port,omitempty"`
	User          string  `json:"user,omitempty"`
	Path          string  `json:"path,omitempty"`
	Password      *string `json:"password,omitempty"`
	ClearPassword bool    `json:"clearPassword,omitempty"`
	HasPassword   bool    `json:"hasPassword,omitempty"`
	sealed        string
}

type Config struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Mode        string      `json:"mode"`
	Folders     []string    `json:"folders"`
	Excludes    []string    `json:"excludes,omitempty"`
	Databases   []Database  `json:"databases"`
	Destination Destination `json:"destination"`
	Logs        *LogConfig  `json:"logs,omitempty"`
	Frequency   Period      `json:"frequency"`
	KeepLast    int         `json:"keepLast"`
	MaxAge      *Period     `json:"maxAge,omitempty"`
	MaxBytes    int64       `json:"maxBytes"`
	Created     string      `json:"created,omitempty"`
	NextDue     string      `json:"nextDue,omitempty"`
	AnchorDay   int         `json:"anchorDay,omitempty"`
	ownerKey    []byte
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("secure randomness unavailable")
	}
	return hex.EncodeToString(b)
}

func text(s string, max int) bool {
	if len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func (p Period) valid() bool {
	if p.Value < 1 {
		return false
	}
	switch p.Unit {
	case "minutes":
		return p.Value <= 527040
	case "hours":
		return p.Value <= 8784
	case "days":
		return p.Value <= 366
	case "months":
		return p.Value <= 12
	}
	return false
}

func (p Period) Next(from time.Time, anchor int) time.Time {
	switch p.Unit {
	case "minutes":
		return from.Add(time.Duration(p.Value) * time.Minute)
	case "hours":
		return from.Add(time.Duration(p.Value) * time.Hour)
	case "days":
		return from.Add(time.Duration(p.Value) * 24 * time.Hour)
	case "months":
		if anchor < 1 || anchor > 31 {
			anchor = from.Day()
		}
		first := time.Date(from.Year(), from.Month()+time.Month(p.Value), 1, from.Hour(), from.Minute(), 0, 0, from.Location())
		last := first.AddDate(0, 1, -1).Day()
		if anchor > last {
			anchor = last
		}
		return time.Date(first.Year(), first.Month(), anchor, first.Hour(), first.Minute(), first.Second(), 0, first.Location())
	}
	return from
}

func cleanDestination(s string) (string, error) {
	if !text(s, 2048) || !strings.HasPrefix(s, "/") || strings.Contains(s, `\`) {
		return "", errors.New("The destination must be an absolute remote path beginning with /.")
	}
	for _, component := range strings.Split(s, "/") {
		if component == ".." {
			return "", errors.New("Parent traversal is not allowed in a destination.")
		}
	}
	return path.Clean(s), nil
}

func validate(c *Config) error {
	if err := validateLogs(c.Logs, false); err != nil {
		return err
	}
	if c.ID != "" && !idPattern.MatchString(c.ID) {
		return errors.New("Invalid backup ID.")
	}
	if !text(c.Name, 100) || strings.TrimSpace(c.Name) == "" {
		return errors.New("Choose a backup name (up to 100 characters).")
	}
	if c.Mode != "folders" && c.Mode != "databases" && c.Mode != "both" {
		return errors.New("Choose folders, databases, or both.")
	}
	if len(c.Folders) > 32 || len(c.Databases) > 16 || len(c.Excludes) > 64 {
		return errors.New("A backup supports up to 32 folders, 16 databases and 64 exclusions.")
	}
	if c.Mode != "databases" && len(c.Folders) == 0 {
		return errors.New("Choose at least one source folder.")
	}
	if c.Mode != "folders" && len(c.Databases) == 0 {
		return errors.New("Choose at least one database.")
	}
	for i, folder := range c.Folders {
		if !text(folder, 4096) || !filepath.IsAbs(folder) {
			return errors.New("Source folders must be absolute paths.")
		}
		c.Folders[i] = filepath.Clean(folder)
	}
	for _, exclusion := range c.Excludes {
		if !text(exclusion, 256) || exclusion == "" || strings.HasPrefix(exclusion, "/") || strings.Contains(exclusion, `\`) {
			return errors.New("Exclusions must be relative names or glob patterns.")
		}
		for _, part := range strings.Split(exclusion, "/") {
			if part == ".." {
				return errors.New("An exclusion cannot contain parent traversal.")
			}
		}
		if _, err := path.Match(strings.TrimSuffix(exclusion, "/**"), "test"); err != nil {
			return errors.New("Invalid exclusion glob.")
		}
	}
	ids := map[string]bool{}
	for i := range c.Databases {
		db := &c.Databases[i]
		if db.ID == "" {
			db.ID = newID()
		}
		if !idPattern.MatchString(db.ID) || ids[db.ID] {
			return errors.New("Invalid or duplicate database ID.")
		}
		ids[db.ID] = true
		if db.Kind != "postgres" && db.Kind != "mysql" && db.Kind != "sqlite" {
			return errors.New("Choose PostgreSQL, MySQL/MariaDB or SQLite.")
		}
		if !databasePattern.MatchString(db.Name) {
			return errors.New("Database names must start with a letter, digit or underscore and contain only letters, digits, spaces, dots, underscores or dashes.")
		}
		if db.Kind == "sqlite" {
			if !text(db.Path, 4096) || !filepath.IsAbs(db.Path) {
				return errors.New("Choose an absolute SQLite database path.")
			}
			db.Path = filepath.Clean(db.Path)
			db.Host = ""
			db.User = ""
			db.Port = 0
			db.Password = nil
			db.ClearPassword = true
		} else {
			if !text(db.Host, 253) || db.Host == "" || strings.ContainsAny(db.Host, " /\\=\"'@") {
				return errors.New("Choose a database hostname or IP address.")
			}
			if net.ParseIP(strings.Trim(db.Host, "[]")) == nil {
				for _, r := range db.Host {
					if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
						return errors.New("Invalid database hostname.")
					}
				}
			}
			if db.Port == 0 {
				db.Port = 5432
				if db.Kind == "mysql" {
					db.Port = 3306
				}
			}
			if db.Port < 1 || db.Port > 65535 || !text(db.User, 128) || db.User == "" {
				return errors.New("Choose a database user and port between 1 and 65535.")
			}
			if db.Password != nil && !text(*db.Password, 4096) {
				return errors.New("Database passwords cannot contain control characters.")
			}
		}
	}
	if err := validateDestination(&c.Destination, false); err != nil {
		return err
	}
	if !c.Frequency.valid() {
		return errors.New("Choose a backup frequency between one minute and twelve months.")
	}
	if c.Destination.externalHTTP() {
		c.KeepLast = 0
		c.MaxAge = nil
	}
	if !c.Destination.externalHTTP() && (c.KeepLast < 1 || c.KeepLast > 365) {
		return errors.New("Keep between 1 and 365 successful backups.")
	}
	if c.MaxAge != nil && !c.MaxAge.valid() {
		return errors.New("Choose a valid retention age.")
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = defaultBytes
	}
	if c.MaxBytes < 1<<20 || c.MaxBytes > 100<<30 {
		return fmt.Errorf("The backup size limit must be between 1 MiB and 100 GiB.")
	}
	return nil
}
