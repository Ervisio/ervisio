package files

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

const trashDateLayout = "2006-01-02T15:04:05"

func trashDirs() (files, info string, err error) {
	if runtime.GOOS == "windows" {
		return "", "", rpc.Errorf(rpc.Unavailable, "The Recycle Bin is not available from here. Delete the items permanently instead.")
	}
	home, err := homeDir()
	if err != nil {
		return "", "", err
	}
	base := filepath.Join(home, ".local", "share", "Trash")
	return filepath.Join(base, "files"), filepath.Join(base, "info"), nil
}

func ensureTrash() (files, info string, err error) {
	files, info, err = trashDirs()
	if err != nil {
		return
	}
	for _, d := range []string{files, info} {
		if err = os.MkdirAll(d, 0o700); err != nil {
			return
		}
	}
	return
}

func encodeTrashPath(p string) string { return (&url.URL{Path: p}).EscapedPath() }

func moveToTrash(p string) error {
	filesDir, infoDir, err := ensureTrash()
	if err != nil {
		return err
	}
	if isInside(p, filepath.Dir(filesDir)) {
		return rpc.Errorf(rpc.Invalid, "This item is already in the Trash.")
	}
	base := filepath.Base(p)
	name := base
	var infoFile *os.File
	for i := 1; ; i++ {
		if i > 1 {
			name = base + "." + time.Now().Format("150405") + "." + strconv.Itoa(i)
		}
		if _, err := os.Lstat(filepath.Join(filesDir, name)); err == nil {
			continue
		}
		f, err := os.OpenFile(filepath.Join(infoDir, name+".trashinfo"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if os.IsExist(err) && i < 1000 {
				continue
			}
			return err
		}
		infoFile = f
		break
	}
	content := "[Trash Info]\nPath=" + encodeTrashPath(p) + "\nDeletionDate=" + time.Now().Format(trashDateLayout) + "\n"
	_, werr := infoFile.WriteString(content)
	infoFile.Close()
	infoPath := filepath.Join(infoDir, name+".trashinfo")
	if werr != nil {
		os.Remove(infoPath)
		return werr
	}
	if err := movePath(p, filepath.Join(filesDir, name)); err != nil {
		os.Remove(infoPath)
		return err
	}
	return nil
}

type trashInfo struct {
	Path string
	Date time.Time
}

func parseTrashInfo(data string) (trashInfo, bool) {
	var ti trashInfo
	ok := false
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Path="):
			if p, err := url.PathUnescape(strings.TrimPrefix(line, "Path=")); err == nil {
				ti.Path = p
				ok = true
			}
		case strings.HasPrefix(line, "DeletionDate="):
			if t, err := time.ParseInLocation(trashDateLayout, strings.TrimPrefix(line, "DeletionDate="), time.Local); err == nil {
				ti.Date = t
			}
		}
	}
	return ti, ok
}

type trashItem struct {
	ID           string `json:"id"`
	OriginalPath string `json:"originalPath"`
	DeletedAt    int64  `json:"deletedAt"` // unix ms, 0 when unknown
	Entry        Entry  `json:"entry"`
}

func hTrashList(ctx context.Context, c *rpc.Call) (any, error) {
	filesDir, infoDir, err := trashDirs()
	if err != nil {
		return nil, err
	}
	items := []trashItem{}
	des, err := os.ReadDir(filesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{"items": items}, nil
		}
		return nil, err
	}
	for _, de := range des {
		fi, err := de.Info()
		if err != nil {
			continue
		}
		e := makeEntry(filesDir, fi)
		e.Path = filepath.Join(filesDir, de.Name())
		it := trashItem{ID: de.Name(), Entry: e}
		if data, err := os.ReadFile(filepath.Join(infoDir, de.Name()+".trashinfo")); err == nil {
			if ti, ok := parseTrashInfo(string(data)); ok {
				it.OriginalPath = ti.Path
				if !ti.Date.IsZero() {
					it.DeletedAt = ti.Date.UnixMilli()
				}
				it.Entry.Name = filepath.Base(ti.Path)
			}
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].DeletedAt > items[j].DeletedAt })
	return map[string]any{"items": items}, nil
}

func trashID(id string) error {
	if err := validName(id); err != nil {
		return rpc.Errorf(rpc.Invalid, "That Trash item is not valid.")
	}
	return nil
}

func hRestore(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		IDs []string `json:"ids"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if len(p.IDs) == 0 {
		return nil, rpc.Errorf(rpc.Invalid, "Nothing was selected.")
	}
	filesDir, infoDir, err := trashDirs()
	if err != nil {
		return nil, err
	}
	restored := []string{}
	failed := []failure{}
	var first error
	for _, id := range p.IDs {
		err := func() error {
			if err := trashID(id); err != nil {
				return err
			}
			data, err := os.ReadFile(filepath.Join(infoDir, id+".trashinfo"))
			if err != nil {
				return rpc.Errorf(rpc.NotFound, "The original location of %q is unknown, so it cannot be restored automatically.", id)
			}
			ti, ok := parseTrashInfo(string(data))
			if !ok {
				return rpc.Errorf(rpc.Invalid, "The Trash record for %q is damaged.", id)
			}
			dest, err := cleanPath(ti.Path)
			if err != nil {
				return err
			}
			if _, err := os.Lstat(dest); err == nil {
				return rpc.Errorf(rpc.Conflict, "%s already exists. Move or rename it, then restore again.", dest)
			}
			if err := mkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			if err := movePath(filepath.Join(filesDir, id), dest); err != nil {
				return err
			}
			os.Remove(filepath.Join(infoDir, id+".trashinfo"))
			restored = append(restored, dest)
			return nil
		}()
		if err != nil {
			if first == nil {
				first = err
			}
			re := rpc.ToError(err, false)
			failed = append(failed, failure{Path: id, Message: re.Message, Code: string(re.Code)})
		}
	}
	if len(restored) == 0 && first != nil {
		return nil, first
	}
	return map[string]any{"restored": restored, "failed": failed}, nil
}

func hTrashDelete(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		IDs []string `json:"ids"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	filesDir, infoDir, err := trashDirs()
	if err != nil {
		return nil, err
	}
	n := 0
	for _, id := range p.IDs {
		if err := trashID(id); err != nil {
			return nil, err
		}
		if err := removePath(filepath.Join(filesDir, id)); err != nil {
			return nil, err
		}
		os.Remove(filepath.Join(infoDir, id+".trashinfo"))
		n++
	}
	return map[string]any{"deleted": n}, nil
}

func hTrashEmpty(ctx context.Context, c *rpc.Call) (any, error) {
	filesDir, infoDir, err := trashDirs()
	if err != nil {
		return nil, err
	}
	n := 0
	for _, d := range []string{filesDir, infoDir} {
		des, err := os.ReadDir(d)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, de := range des {
			if err := removePath(filepath.Join(d, de.Name())); err != nil {
				return nil, err
			}
			if d == filesDir {
				n++
			}
		}
	}
	return map[string]any{"deleted": n}, nil
}
