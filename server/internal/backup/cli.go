package backup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ervisio/ervisio/server/internal/modules/remote"
)

type response struct {
	OK     bool           `json:"ok"`
	Result any            `json:"result,omitempty"`
	Error  *responseError `json:"error,omitempty"`
}
type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RunCLI is a standalone user-level worker, not a daemon or another scheduler.
// The caller must dispatch this before starting Ervisio's server flags.
func RunCLI(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 6*time.Hour)
	defer cancel()
	return runCLI(ctx, args, os.Stdout, &engine{})
}

// Run is retained as a small alias for embedders using the initial CLI contract.
func Run(args []string) int { return RunCLI(args) }

func runCLI(ctx context.Context, args []string, out io.Writer, e *engine) int {
	result, err := dispatch(ctx, args, e)
	r := response{OK: err == nil, Result: result}
	code := 0
	if err != nil {
		r.Result = nil
		r.Error = &responseError{Code: "backup_failed", Message: safeRunError(err)}
		code = 1
	}
	if json.NewEncoder(out).Encode(r) != nil {
		return 1
	}
	return code
}

func dispatch(ctx context.Context, args []string, e *engine) (any, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, errors.New("Use --backup status, list, remotes, save, delete, run, tick, history, test or browse.")
	}
	method := args[0]
	needsID := method == "save" || method == "delete" || method == "run" || method == "tick" || method == "history" || method == "test" || method == "browse"
	if needsID && (len(args) != 2 || !idPattern.MatchString(args[1])) {
		return nil, errors.New("This backup method requires one 32-character hexadecimal ID.")
	}
	if !needsID && (len(args) != 1 || (method != "status" && method != "list" && method != "remotes")) {
		return nil, errors.New("Unknown backup method or unexpected arguments.")
	}
	if runtime.GOOS != "linux" {
		if method == "status" {
			return map[string]any{"supported": false, "platform": runtime.GOOS, "reason": "The Backup worker currently supports Linux."}, nil
		}
		return nil, errors.New("The Backup worker currently supports Linux.")
	}
	if err := e.init(); err != nil {
		return nil, err
	}
	s := &e.store
	switch method {
	case "status":
		clients := map[string]bool{}
		for _, client := range []string{"pg_dump", "mysqldump", "mariadb-dump", "python3"} {
			_, err := exec.LookPath(client)
			clients[client] = err == nil
		}
		free, _ := availableBytes(s.dir)
		account := ""
		if current, err := user.Current(); err == nil {
			account = current.Username
		}
		return map[string]any{"supported": true, "platform": "linux", "account": account, "clients": clients, "spoolFree": free, "spoolReserve": reserveBytes, "maxPlans": maxJobs, "maxFolders": 32, "maxDatabases": 16}, nil
	case "list":
		items, err := s.list()
		return map[string]any{"plans": items}, err
	case "remotes":
		items, err := remote.BackupConnections()
		return map[string]any{"connections": items}, err
	case "save":
		var c Config
		if err := s.inbox(args[1], &c); err != nil {
			return nil, err
		}
		if err := s.save(&c); err != nil {
			return nil, err
		}
		return view(&c), nil
	case "history":
		items, err := s.history(args[1])
		return map[string]any{"runs": items}, err
	case "run", "tick":
		return e.run(ctx, args[1], method == "tick")
	case "delete":
		unlock, err := lockFile(filepath.Join(s.dir, "run.lock"))
		if err != nil {
			return nil, err
		}
		defer unlock()
		configUnlock, err := lockFile(filepath.Join(s.dir, "config.lock"))
		if err != nil {
			return nil, err
		}
		defer configUnlock()
		c, loadErr := s.load(args[1])
		if loadErr != nil {
			return nil, loadErr
		}
		if err = cleanOwnedSpools(s, c); err != nil {
			return nil, err
		}
		if err = os.Remove(filepath.Join(s.dir, "state", args[1]+".json")); err != nil {
			return nil, err
		}
		_ = os.Remove(filepath.Join(s.dir, "state", args[1]+".history"))
		return map[string]any{"deleted": true, "remoteBackupsPreserved": true}, nil
	case "test":
		c, err := s.load(args[1])
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		err = e.withRemote(ctx, c.Destination.Connection, func(ctx context.Context, f remote.FS) error {
			entry, err := f.Stat(ctx, c.Destination.Path)
			if err != nil {
				return err
			}
			if entry.Type != "dir" {
				return errors.New("The destination is not a real directory.")
			}
			return nil
		})
		return map[string]any{"reachable": err == nil, "path": c.Destination.Path, "writeTested": false}, err
	case "browse":
		return e.browse(ctx, args[1])
	}
	return nil, errors.New("Unknown backup method.")
}

func (e *engine) browse(ctx context.Context, token string) (any, error) {
	var p struct {
		Connection string `json:"connection"`
		Path       string `json:"path"`
	}
	if err := e.store.inbox(token, &p); err != nil {
		return nil, err
	}
	if !connectionPattern.MatchString(p.Connection) {
		return nil, errors.New("Choose a saved connection.")
	}
	clean, err := cleanDestination(p.Path)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	dirs := []remote.Entry{}
	err = e.withRemote(ctx, p.Connection, func(ctx context.Context, f remote.FS) error {
		for prefix := "/"; prefix != ""; { // Verify each component instead of following a directory link.
			entry, err := f.Stat(ctx, prefix)
			if err != nil {
				return err
			}
			if entry.Type != "dir" {
				return errors.New("Remote folder links cannot be used as backup destinations.")
			}
			if prefix == clean {
				break
			}
			rest := strings.TrimPrefix(strings.TrimPrefix(clean, prefix), "/")
			part, _, _ := strings.Cut(rest, "/")
			if part == "" {
				break
			}
			if prefix == "/" {
				prefix += part
			} else {
				prefix += "/" + part
			}
		}
		items, err := f.List(ctx, clean)
		if err != nil {
			return err
		}
		for _, entry := range items {
			if entry.Type == "dir" && text(entry.Name, 255) && !strings.ContainsAny(entry.Name, "/\\") {
				dirs = append(dirs, entry)
			}
			if len(dirs) >= 500 {
				break
			}
		}
		return nil
	})
	return map[string]any{"path": clean, "directories": dirs, "limit": 500}, err
}
