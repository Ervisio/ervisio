package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
)

type storedConfig struct {
	Config
	Secrets   map[string]string `json:"secrets,omitempty"`
	Ownership string            `json:"ownership"`
}

type store struct{ dir string }

func (s *store) init() error {
	if s.dir == "" {
		home, err := os.UserHomeDir()
		if err != nil || !filepath.IsAbs(home) {
			return errors.New("The account home folder is unavailable.")
		}
		s.dir = filepath.Join(home, filepath.FromSlash(brand.UserDataDir), "plugins", "backup")
	}
	for _, dir := range []string{s.dir, filepath.Join(s.dir, "inbox"), filepath.Join(s.dir, "state")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return errors.New("The private backup folder cannot be created.")
		}
		fi, err := os.Lstat(dir)
		if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || !privateOwner(fi) {
			return errors.New("The backup folder must be a private directory owned by this account.")
		}
	}
	return nil
}

func readPrivate(p string, max int64) ([]byte, error) {
	f, err := openProtected(p, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || !privateOwner(fi) || fi.Size() > max {
		return nil, errors.New("A backup settings file is not a private, bounded regular file.")
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("Backup settings exceed the size limit.")
	}
	return b, nil
}

func decodeStrict(b []byte, out any) error {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errors.New("The backup settings are not valid JSON or contain unsupported fields.")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("The backup request must contain one JSON object.")
	}
	return nil
}

func (s *store) inbox(token string, out any) error {
	if !idPattern.MatchString(token) {
		return errors.New("Invalid private request token.")
	}
	p := filepath.Join(s.dir, "inbox", token+".json")
	b, err := readPrivate(p, maxInput)
	// The one-shot secret-bearing input never remains after a consumed request.
	if err == nil {
		if err = os.Remove(p); err != nil {
			return errors.New("The private input could not be removed.")
		}
	}
	if err != nil {
		return errors.New("The private request file cannot be read safely.")
	}
	return decodeStrict(b, out)
}

func atomicPrivate(p string, b []byte) error {
	if fi, err := os.Lstat(p); err == nil {
		if !fi.Mode().IsRegular() || !privateOwner(fi) {
			return errors.New("Unsafe backup state file.")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".backup-state-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (s *store) aead() (cipher.AEAD, error) {
	p := filepath.Join(s.dir, "backup.key")
	key, err := readPrivate(p, 32)
	if errors.Is(err, fs.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, e := openProtected(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(e, fs.ErrExist) {
			key, err = readPrivate(p, 32)
		} else if e != nil {
			return nil, e
		} else {
			_, err = f.Write(key)
			if e = f.Close(); err == nil {
				err = e
			}
		}
	}
	if err != nil || len(key) != 32 {
		return nil, errors.New("The private backup encryption key is unavailable or damaged.")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(g cipher.AEAD, plain []byte, aad string) (string, error) {
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return "v1:" + base64.RawStdEncoding.EncodeToString(g.Seal(nonce, nonce, plain, []byte(aad))), nil
}

func unseal(g cipher.AEAD, sealed, aad string) ([]byte, error) {
	if !strings.HasPrefix(sealed, "v1:") {
		return nil, errors.New("Unsupported backup secret format.")
	}
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(sealed, "v1:"))
	if err != nil || len(b) < g.NonceSize() {
		return nil, errors.New("Invalid backup secret.")
	}
	p, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], []byte(aad))
	if err != nil {
		return nil, errors.New("The backup secret cannot be decrypted by this account.")
	}
	return p, nil
}

func (s *store) load(id string) (*Config, error) {
	if !idPattern.MatchString(id) {
		return nil, errors.New("Invalid backup ID.")
	}
	b, err := readPrivate(filepath.Join(s.dir, "state", id+".json"), maxInput)
	if err != nil {
		return nil, errors.New("This backup configuration is unavailable.")
	}
	var record storedConfig
	if err = decodeStrict(b, &record); err != nil {
		return nil, err
	}
	c := record.Config
	if c.ID != id {
		return nil, errors.New("The backup configuration ID does not match its file.")
	}
	g, err := s.aead()
	if err != nil {
		return nil, err
	}
	c.ownerKey, err = unseal(g, record.Ownership, id+":ownership")
	if err != nil || len(c.ownerKey) != 32 {
		return nil, errors.New("The backup ownership key cannot be read.")
	}
	for i := range c.Databases {
		db := &c.Databases[i]
		db.sealed = record.Secrets[db.ID]
		if db.sealed != "" {
			plain, e := unseal(g, db.sealed, id+":"+db.ID)
			if e != nil {
				return nil, e
			}
			password := string(plain)
			db.Password = &password
			db.HasPassword = password != ""
		}
	}
	return &c, nil
}

func view(c *Config) Config {
	v := *c
	v.ownerKey = nil
	v.Databases = append([]Database{}, c.Databases...)
	for i := range v.Databases {
		v.Databases[i].HasPassword = v.Databases[i].Password != nil && *v.Databases[i].Password != ""
		v.Databases[i].Password = nil
		v.Databases[i].ClearPassword = false
		v.Databases[i].sealed = ""
	}
	return v
}

func (s *store) write(c *Config) error {
	g, err := s.aead()
	if err != nil {
		return err
	}
	r := storedConfig{Config: view(c), Secrets: map[string]string{}}
	r.Ownership, err = seal(g, c.ownerKey, c.ID+":ownership")
	if err != nil {
		return err
	}
	for _, db := range c.Databases {
		if db.Password != nil && *db.Password != "" {
			r.Secrets[db.ID], err = seal(g, []byte(*db.Password), c.ID+":"+db.ID)
			if err != nil {
				return err
			}
		}
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivate(filepath.Join(s.dir, "state", c.ID+".json"), append(b, '\n'))
}

func (s *store) list() ([]Config, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "state"))
	if err != nil {
		return nil, err
	}
	items := []Config{}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".json")
		if !idPattern.MatchString(id) || e.IsDir() {
			continue
		}
		c, err := s.load(id)
		if err != nil {
			return nil, err
		}
		items = append(items, view(c))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (s *store) save(c *Config) error {
	unlock, err := lockFile(filepath.Join(s.dir, "config.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if err := validate(c); err != nil {
		return err
	}
	now := time.Now()
	var old *Config
	if c.ID != "" {
		old, err = s.load(c.ID)
		if err != nil {
			return err
		}
	} else {
		items, err := s.list()
		if err != nil {
			return err
		}
		if len(items) >= maxJobs {
			return errors.New("This account already has 20 backup plans (up to 40 scheduled/manual job instances).")
		}
		c.ID = newID()
	}
	if old != nil {
		c.ownerKey = old.ownerKey
		c.Created = old.Created
		c.AnchorDay = old.AnchorDay
		c.NextDue = old.NextDue
		for i := range c.Databases {
			db := &c.Databases[i]
			for _, previous := range old.Databases {
				if db.ID == previous.ID && (db.Password == nil || *db.Password == "") && !db.ClearPassword {
					db.Password = previous.Password
				}
			}
		}
		if c.Frequency != old.Frequency {
			c.NextDue = now.Format(time.RFC3339)
			if c.Frequency.Unit == "months" {
				c.NextDue = now.Truncate(time.Minute).Format(time.RFC3339)
			}
			c.AnchorDay = now.Day()
		}
	} else {
		c.ownerKey = make([]byte, 32)
		if _, err = rand.Read(c.ownerKey); err != nil {
			return err
		}
		c.Created = now.Format(time.RFC3339)
		c.NextDue = c.Created
		if c.Frequency.Unit == "months" {
			c.NextDue = now.Truncate(time.Minute).Format(time.RFC3339)
		}
		c.AnchorDay = now.Day()
	}
	for i := range c.Databases {
		if c.Databases[i].ClearPassword {
			c.Databases[i].Password = nil
		}
	}
	return s.write(c)
}
