package backup

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ervisio/ervisio/server/internal/modules/remote"
)

func (e *engine) publishDestination(ctx context.Context, c *Config, local, hash string, info archiveInfo) (string, []string, error) {
	switch c.Destination.kind() {
	case "s3":
		return publishS3(ctx, c, local, hash, info)
	case "http", "drive", "repo":
		return publishHTTP(ctx, c, local, hash, info)
	}
	var archive string
	var warnings []string
	work := func(ctx context.Context, f remote.FS, effective *Config) error {
		journal := &uploadJournal{store: &e.store, config: effective}
		if err := journal.recover(ctx, f); err != nil {
			return err
		}
		ctx = context.WithValue(ctx, journalKey{}, journal)
		var err error
		archive, warnings, err = publish(ctx, f, effective, local, hash, info)
		return err
	}
	if c.Destination.kind() == "local" {
		f, effective, err := openLocalDestination(c)
		if err != nil {
			return "", nil, err
		}
		defer f.Close()
		err = work(ctx, f, effective)
		if err == nil {
			archive = filepath.Join(c.Destination.Path, filepath.FromSlash(strings.TrimPrefix(archive, "/")))
		}
		return archive, warnings, err
	}
	err := e.withRemote(ctx, c.Destination.Connection, func(ctx context.Context, f remote.FS) error { return work(ctx, f, c) })
	return archive, warnings, err
}
