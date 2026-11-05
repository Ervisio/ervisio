// Package maintenance implements an explicitly planned, backed-up Ervisio data reset.
package maintenance

import "context"

type Options struct {
	ConfigPath     string   `json:"configPath"`
	StateDir       string   `json:"stateDir"`
	CacheDir       string   `json:"cacheDir"`
	AIStateDir     string   `json:"aiStateDir,omitempty"`
	BackupDir      string   `json:"backupDir"`
	CurrentURL     string   `json:"currentURL"`
	Operator       string   `json:"operator"`
	Homes          []string `json:"homes"`
	PreserveAccess bool     `json:"preserveAccess"`
}
type Target struct {
	Path   string `json:"path"`
	Files  int64  `json:"files"`
	Bytes  int64  `json:"bytes"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}
type identity struct {
	Device, Inode  uint64
	UID, GID, Mode uint32
	Size, Modified int64
}

// Plan is a private helper document. Config and Options must never be returned by a public API.
type Plan struct {
	ID          string   `json:"id"`
	CreatedAt   int64    `json:"createdAt"`
	Fingerprint string   `json:"fingerprint"`
	Options     Options  `json:"options"`
	Paths       []Target `json:"paths"`
	Preserved   []string `json:"preserved"`
	Files       int64    `json:"files"`
	Bytes       int64    `json:"bytes"`
	NextURL     string   `json:"nextURL"`
	Config      []byte   `json:"config"`
	inventory   map[string]identity
}
type Result struct {
	Backup  string `json:"backup"`
	NextURL string `json:"nextURL"`
}
type Service interface {
	Stop(context.Context) error
	Start(context.Context) error
}
