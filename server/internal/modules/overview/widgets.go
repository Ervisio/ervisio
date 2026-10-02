package overview

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// UnitStatus is one row of overview.unitStatus.
type UnitStatus struct {
	Unit        string `json:"unit"`
	Description string `json:"description"`
	Active      string `json:"active"` // active | inactive | failed | activating | deactivating | not-found
	Sub         string `json:"sub"`
	Enabled     string `json:"enabled,omitempty"`
}

var unitRe = regexp.MustCompile(`^[A-Za-z0-9:_.@\\-]{1,200}$`)

func normalizeUnit(u string) (string, bool) {
	u = strings.TrimSpace(u)
	if !unitRe.MatchString(u) || strings.HasPrefix(u, "-") {
		return "", false
	}
	if !strings.Contains(u, ".") {
		u += ".service"
	}
	return u, true
}

func unitStatus(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Units []string `json:"units"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if len(p.Units) == 0 || len(p.Units) > 30 {
		return nil, rpc.Errorf(rpc.Invalid, "Pick between 1 and 30 services.")
	}
	out := make([]UnitStatus, 0, len(p.Units))
	for _, raw := range p.Units {
		u, ok := normalizeUnit(raw)
		if !ok {
			return nil, rpc.Errorf(rpc.Invalid, "%q is not a valid unit name.", raw)
		}
		b, err := sys.Output(ctx, "systemctl", "show", "--no-pager", "--property=Description,LoadState,ActiveState,SubState,UnitFileState", "--", u)
		if err != nil {
			return nil, err
		}
		out = append(out, parseUnitShow(u, string(b)))
	}
	return out, nil
}

func parseUnitShow(unit, show string) UnitStatus {
	kv := map[string]string{}
	for _, l := range strings.Split(show, "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			kv[k] = strings.TrimSpace(v)
		}
	}
	s := UnitStatus{Unit: unit, Description: kv["Description"], Active: kv["ActiveState"], Sub: kv["SubState"], Enabled: kv["UnitFileState"]}
	if kv["LoadState"] == "not-found" {
		s.Active, s.Sub, s.Description = "not-found", "not-found", ""
	}
	return s
}

const maxTailBytes = 256 << 10

// logTail returns the last lines of a unit's journal, the system journal or a file.
func logTail(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Unit  string `json:"unit"`
		File  string `json:"file"`
		Lines int    `json:"lines"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if p.Lines <= 0 {
		p.Lines = 12
	}
	if p.Lines > 200 {
		p.Lines = 200
	}
	if p.File != "" {
		return tailFile(p.File, p.Lines)
	}
	args := []string{"--no-pager", "-q", "-o", "short-iso", "-n", strconv.Itoa(p.Lines)}
	if p.Unit != "" {
		u, ok := normalizeUnit(p.Unit)
		if !ok {
			return nil, rpc.Errorf(rpc.Invalid, "%q is not a valid unit name.", p.Unit)
		}
		args = append(args, "-u", u)
	}
	out, err := sys.Output(ctx, "journalctl", args...)
	if err != nil {
		if ee, ok := err.(*sys.ExitError); ok {
			return nil, rpc.Errorf(rpc.NeedsAdmin, "The journal is not readable by this user: %s", strings.TrimSpace(ee.Stderr))
		}
		return nil, err
	}
	return map[string]any{"lines": splitLines(string(out), p.Lines)}, nil
}

func tailFile(path string, n int) (any, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return nil, rpc.Errorf(rpc.Invalid, "The log file path must be absolute.")
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err // permission and not-found map to codes automatically
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, rpc.Errorf(rpc.Invalid, "%s is a folder, not a file.", path)
	}
	off := int64(0)
	if fi.Size() > maxTailBytes {
		off = fi.Size() - maxTailBytes
	}
	buf := make([]byte, fi.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return nil, err
	}
	return map[string]any{"lines": splitLines(strings.ToValidUTF8(string(buf), "�"), n)}, nil
}

func splitLines(s string, n int) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return []string{}
	}
	l := strings.Split(s, "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return l
}
