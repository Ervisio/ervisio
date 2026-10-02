// Package notify sends notifications to the channels an administrator
// configured in Settings: email (SMTP), Telegram, a generic webhook, ntfy
// and Gotify. The core's alerts, plugins (plugins.notify) and plugin job
// steps all go through Service.Send.
//
// Channels live in one root-only file (0600) that holds their secrets. The
// browser never gets a secret back: Channel, the type every API returns,
// has no secret fields, only flags that say one is set.
package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Channel types.
const (
	TypeEmail    = "email"
	TypeTelegram = "telegram"
	TypeWebhook  = "webhook"
	TypeNtfy     = "ntfy"
	TypeGotify   = "gotify"
)

// Events a channel can subscribe to.
const (
	// EventUpdates: a new Ervisio release is available.
	EventUpdates = "updates"
	// EventPlugins: notifications sent by plugins (plugins.notify, job notify steps).
	EventPlugins = "plugins"
	// EventJobs: a plugin job failed or recovered.
	EventJobs = "jobs"
)

// Events lists them all; a new channel subscribes to every one.
var Events = []string{EventUpdates, EventPlugins, EventJobs}

// Levels, lowest first. "success" ranks as info.
var levelRank = map[string]int{"info": 0, "success": 0, "warn": 1, "error": 2}

// ValidLevel reports whether s is a notification level.
func ValidLevel(s string) bool { _, ok := levelRank[s]; return ok }

const (
	maxChannels = 20
	maxTemplate = 4096
)

var (
	hostRe       = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$|^\[[0-9A-Fa-f:.]+\]$`)
	tgTokenRe    = regexp.MustCompile(`^[0-9]{3,20}:[A-Za-z0-9_-]{20,80}$`)
	tgChatRe     = regexp.MustCompile(`^(-?[0-9]{1,20}|@[A-Za-z0-9_]{4,64})$`)
	topicRe      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	headerNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)
	idRe         = regexp.MustCompile(`^[a-f0-9]{8}$`)
	forbiddenHdr = map[string]bool{"host": true, "content-length": true, "transfer-encoding": true, "connection": true, "content-type": true, "cookie": true}
)

// DefaultTemplate is the JSON body of a webhook channel without a template.
const DefaultTemplate = `{"title":"{title}","body":"{body}","level":"{level}","link":"{link}","source":"{source}","time":"{time}"}`

// Spec is a channel as stored in the file and as the browser submits it. It
// holds the secrets: it is never sent to the browser (see Channel).
type Spec struct {
	ID       string   `json:"id,omitempty"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Enabled  bool     `json:"enabled"`
	MinLevel string   `json:"minLevel,omitempty"`
	Events   []string `json:"events"`

	// email
	Host     string   `json:"host,omitempty"`
	Port     int      `json:"port,omitempty"`
	Security string   `json:"security,omitempty"` // none, starttls, tls
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"` // secret
	From     string   `json:"from,omitempty"`
	To       []string `json:"to,omitempty"`

	// telegram
	BotToken string `json:"botToken,omitempty"` // secret
	ChatID   string `json:"chatId,omitempty"`

	// webhook
	URL         string `json:"url,omitempty"` // secret (service URLs carry tokens)
	Method      string `json:"method,omitempty"`
	Template    string `json:"template,omitempty"`
	HeaderName  string `json:"headerName,omitempty"`
	HeaderValue string `json:"headerValue,omitempty"` // secret

	// ntfy, gotify
	Server string `json:"server,omitempty"`
	Topic  string `json:"topic,omitempty"`
	Token  string `json:"token,omitempty"` // secret
}

// Channel is what the browser sees: a Spec without its secrets.
type Channel struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Enabled  bool     `json:"enabled"`
	MinLevel string   `json:"minLevel"`
	Events   []string `json:"events"`

	Host       string   `json:"host,omitempty"`
	Port       int      `json:"port,omitempty"`
	Security   string   `json:"security,omitempty"`
	Username   string   `json:"username,omitempty"`
	From       string   `json:"from,omitempty"`
	To         []string `json:"to,omitempty"`
	ChatID     string   `json:"chatId,omitempty"`
	Method     string   `json:"method,omitempty"`
	Template   string   `json:"template,omitempty"`
	HeaderName string   `json:"headerName,omitempty"`
	Server     string   `json:"server,omitempty"`
	Topic      string   `json:"topic,omitempty"`
	// URLHint is the webhook URL cut to scheme and host.
	URLHint string `json:"urlHint,omitempty"`
	// Secrets says which secret fields hold a value (password, botToken,
	// url, headerValue, token).
	Secrets map[string]bool `json:"secrets"`
	// Last is the result of the last delivery or test since the daemon started.
	Last *Result `json:"last,omitempty"`
}

// Result is the outcome of a delivery.
type Result struct {
	At    int64  `json:"at"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// secretFields returns the secret fields of the channel's type.
func (s *Spec) secretFields() map[string]*string {
	switch s.Type {
	case TypeEmail:
		return map[string]*string{"password": &s.Password}
	case TypeTelegram:
		return map[string]*string{"botToken": &s.BotToken}
	case TypeWebhook:
		return map[string]*string{"url": &s.URL, "headerValue": &s.HeaderValue}
	case TypeNtfy, TypeGotify:
		return map[string]*string{"token": &s.Token}
	}
	return nil
}

// Secrets lists the non-empty secret values (for redacting error messages).
func (s *Spec) secretValues() []string {
	var out []string
	for _, p := range s.secretFields() {
		if len(*p) >= 4 {
			out = append(out, *p)
		}
	}
	return out
}

// View is the browser's copy of the channel.
func (s *Spec) View(last *Result) Channel {
	c := Channel{
		ID: s.ID, Name: s.Name, Type: s.Type, Enabled: s.Enabled, MinLevel: s.minLevel(), Events: append([]string{}, s.Events...),
		Host: s.Host, Port: s.Port, Security: s.Security, Username: s.Username, From: s.From, To: s.To, ChatID: s.ChatID,
		Method: s.Method, Template: s.Template, HeaderName: s.HeaderName, Server: s.Server, Topic: s.Topic,
		Secrets: map[string]bool{}, Last: last,
	}
	for k, p := range s.secretFields() {
		c.Secrets[k] = *p != ""
	}
	if s.Type == TypeWebhook {
		if u, err := url.Parse(s.URL); err == nil && u.Host != "" {
			c.URLHint = u.Scheme + "://" + u.Host
		}
	}
	return c
}

func (s *Spec) minLevel() string {
	if s.MinLevel == "" {
		return "info"
	}
	return s.MinLevel
}

// keepSecrets copies the secrets that the new spec leaves empty from old,
// so editing a channel without retyping its password keeps it. A different
// type keeps nothing.
func (s *Spec) keepSecrets(old *Spec) {
	if old == nil || old.Type != s.Type {
		return
	}
	oldF := old.secretFields()
	for k, p := range s.secretFields() {
		if *p == "" {
			*p = *oldF[k]
		}
	}
}

// Normalise fills defaults, trims text and clears the fields that belong to
// other channel types.
func (s *Spec) Normalise() {
	s.Name = strings.TrimSpace(s.Name)
	if s.MinLevel == "" {
		s.MinLevel = "info"
	}
	if s.Events == nil {
		s.Events = append([]string{}, Events...)
	}
	keep := *s
	*s = Spec{ID: keep.ID, Name: keep.Name, Type: keep.Type, Enabled: keep.Enabled, MinLevel: keep.MinLevel, Events: keep.Events}
	switch keep.Type {
	case TypeEmail:
		s.Host, s.Port, s.Security, s.Username, s.Password, s.From, s.To = strings.TrimSpace(keep.Host), keep.Port, keep.Security, strings.TrimSpace(keep.Username), keep.Password, strings.TrimSpace(keep.From), keep.To
		if s.Security == "" {
			s.Security = "starttls"
		}
		if s.Port == 0 {
			s.Port = map[string]int{"tls": 465, "starttls": 587, "none": 25}[s.Security]
		}
		for i := range s.To {
			s.To[i] = strings.TrimSpace(s.To[i])
		}
	case TypeTelegram:
		s.BotToken, s.ChatID = strings.TrimSpace(keep.BotToken), strings.TrimSpace(keep.ChatID)
	case TypeWebhook:
		s.URL, s.Method, s.Template, s.HeaderName, s.HeaderValue = strings.TrimSpace(keep.URL), keep.Method, keep.Template, strings.TrimSpace(keep.HeaderName), keep.HeaderValue
		if s.Method == "" {
			s.Method = "POST"
		}
		if s.Template == "" {
			s.Template = DefaultTemplate
		}
	case TypeNtfy:
		s.Server, s.Topic, s.Token = strings.TrimRight(strings.TrimSpace(keep.Server), "/"), strings.TrimSpace(keep.Topic), strings.TrimSpace(keep.Token)
		if s.Server == "" {
			s.Server = "https://ntfy.sh"
		}
	case TypeGotify:
		s.Server, s.Token = strings.TrimRight(strings.TrimSpace(keep.Server), "/"), strings.TrimSpace(keep.Token)
	}
}

func httpURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, errors.New("must be an http:// or https:// address without a user name or password")
	}
	return u, nil
}

// Validate checks a normalised spec (secrets already merged).
func (s *Spec) Validate() error {
	if n := s.Name; n == "" || len(n) > 60 {
		return errors.New("Give the channel a name of up to 60 characters.")
	}
	if _, ok := levelRank[s.MinLevel]; !ok || s.MinLevel == "success" {
		return errors.New("The minimum level must be info, warn or error.")
	}
	for _, e := range s.Events {
		ok := false
		for _, k := range Events {
			ok = ok || k == e
		}
		if !ok {
			return fmt.Errorf("%q is not an event a channel can send.", e)
		}
	}
	switch s.Type {
	case TypeEmail:
		if !hostRe.MatchString(s.Host) {
			return errors.New("Give the SMTP server's host name.")
		}
		if s.Port < 1 || s.Port > 65535 {
			return errors.New("The SMTP port must be between 1 and 65535.")
		}
		if s.Security != "none" && s.Security != "starttls" && s.Security != "tls" {
			return errors.New("Security must be none, starttls or tls.")
		}
		if strings.ContainsAny(s.Username, "\r\n\x00") || strings.ContainsAny(s.Password, "\r\n\x00") || len(s.Username) > 256 || len(s.Password) > 512 {
			return errors.New("The user name or password is not valid.")
		}
		if (s.Username == "") != (s.Password == "") {
			return errors.New("Give both the user name and the password, or neither.")
		}
		if _, err := mail.ParseAddress(s.From); err != nil || strings.ContainsAny(s.From, "\r\n") {
			return errors.New("The From address is not valid.")
		}
		if len(s.To) < 1 || len(s.To) > 10 {
			return errors.New("Give between 1 and 10 recipient addresses.")
		}
		for _, a := range s.To {
			if _, err := mail.ParseAddress(a); err != nil || strings.ContainsAny(a, "\r\n") {
				return fmt.Errorf("%q is not a valid email address.", a)
			}
		}
	case TypeTelegram:
		if !tgTokenRe.MatchString(s.BotToken) {
			return errors.New("The bot token looks like 123456789:AA... (from @BotFather).")
		}
		if !tgChatRe.MatchString(s.ChatID) {
			return errors.New("The chat id is a number (negative for groups) or @channelname.")
		}
	case TypeWebhook:
		if _, err := httpURL(s.URL); err != nil {
			return fmt.Errorf("The webhook URL %v.", err)
		}
		if s.Method != "POST" && s.Method != "PUT" && s.Method != "PATCH" {
			return errors.New("The method must be POST, PUT or PATCH.")
		}
		if len(s.Template) > maxTemplate {
			return errors.New("The template is too long.")
		}
		body, err := renderWebhook(s.Template, sampleVars())
		if err != nil || !json.Valid([]byte(body)) {
			return errors.New("The template must be JSON once {title}, {body}, {level}, {link}, {source} and {time} are replaced.")
		}
		if s.HeaderName != "" {
			if !headerNameRe.MatchString(s.HeaderName) || forbiddenHdr[strings.ToLower(s.HeaderName)] {
				return errors.New("That header name cannot be used.")
			}
			if s.HeaderValue == "" || len(s.HeaderValue) > 2048 || strings.ContainsAny(s.HeaderValue, "\r\n\x00") {
				return errors.New("Give the header's value (up to 2048 characters, one line).")
			}
		} else if s.HeaderValue != "" {
			return errors.New("Give the header's name.")
		}
	case TypeNtfy:
		if _, err := httpURL(s.Server); err != nil {
			return fmt.Errorf("The ntfy server %v.", err)
		}
		if !topicRe.MatchString(s.Topic) {
			return errors.New("The topic is letters, digits, - or _ (up to 64).")
		}
		if strings.ContainsAny(s.Token, "\r\n\x00 ") {
			return errors.New("The access token is not valid.")
		}
	case TypeGotify:
		if _, err := httpURL(s.Server); err != nil {
			return fmt.Errorf("The Gotify server %v.", err)
		}
		if s.Token == "" || strings.ContainsAny(s.Token, "\r\n\x00 ") {
			return errors.New("Give the Gotify application token.")
		}
	default:
		return fmt.Errorf("%q is not a channel type.", s.Type)
	}
	return nil
}

// ---- storage ----

type fileFormat struct {
	Version  int    `json:"version"`
	Channels []Spec `json:"channels"`
}

// Store is the channels file.
type Store struct {
	Path string
	mu   sync.Mutex
}

// Load reads the channels; a missing file has none.
func (st *Store) Load() ([]Spec, error) {
	b, err := os.ReadFile(st.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f fileFormat
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %v", st.Path, err)
	}
	return f.Channels, nil
}

func (st *Store) write(list []Spec) error {
	b, err := json.MarshalIndent(fileFormat{Version: 1, Channels: list}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.Path), 0o755); err != nil {
		return err
	}
	// Created 0600 before any secret is written, renamed over the old file.
	tmp, err := os.OpenFile(st.Path+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), st.Path)
}

// Save stores a channel: a new one when spec.ID is empty, else it replaces
// that channel (empty secrets keep their stored value). It returns the
// stored spec.
func (st *Store) Save(spec Spec) (Spec, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	list, err := st.Load()
	if err != nil {
		return Spec{}, err
	}
	idx := -1
	for i := range list {
		if list[i].ID == spec.ID && spec.ID != "" {
			idx = i
		}
	}
	if spec.ID != "" && idx < 0 {
		return Spec{}, ErrNotFound
	}
	if idx >= 0 {
		spec.keepSecrets(&list[idx])
	}
	spec.Normalise()
	if err := spec.Validate(); err != nil {
		return Spec{}, err
	}
	if idx < 0 {
		if len(list) >= maxChannels {
			return Spec{}, fmt.Errorf("At most %d channels.", maxChannels)
		}
		spec.ID = newID(list)
		list = append(list, spec)
	} else {
		list[idx] = spec
	}
	if err := st.write(list); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// Delete removes a channel.
func (st *Store) Delete(id string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	list, err := st.Load()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].ID == id {
			return st.write(append(list[:i:i], list[i+1:]...))
		}
	}
	return ErrNotFound
}

// Get returns one channel.
func (st *Store) Get(id string) (*Spec, error) {
	list, err := st.Load()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, ErrNotFound
}

// ErrNotFound: no channel has that id.
var ErrNotFound = errors.New("There is no such channel.")

// hostPort is for tests and logs.
func hostPort(host string, port int) string {
	return net.JoinHostPort(strings.Trim(host, "[]"), fmt.Sprint(port))
}
