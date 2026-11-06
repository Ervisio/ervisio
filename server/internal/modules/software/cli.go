package software

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

type cliSpec struct {
	ID, Title, Command, NPM, Docs string
	Packages                      map[string]string
}

var cliCatalog = []cliSpec{
	{ID: "node-lts", Title: "Node.js LTS", Command: "node", Docs: "https://nodejs.org/en/download"},
	{ID: "claude-code", Title: "Claude Code", Command: "claude", NPM: "@anthropic-ai/claude-code", Docs: "https://code.claude.com/docs/en/setup"},
	{ID: "codex", Title: "Codex CLI", Command: "codex", NPM: "@openai/codex", Docs: "https://developers.openai.com/codex/cli/"},
	{ID: "git", Title: "Git", Command: "git", Docs: "https://git-scm.com/downloads", Packages: map[string]string{"apt": "git", "pacman": "git", "dnf": "git", "zypper": "git", "winget": "Git.Git"}},
	{ID: "rclone", Title: "rclone", Command: "rclone", Docs: "https://rclone.org/install/", Packages: map[string]string{"apt": "rclone", "pacman": "rclone", "dnf": "rclone", "zypper": "rclone", "winget": "Rclone.Rclone"}},
	{ID: "restic", Title: "restic", Command: "restic", Docs: "https://restic.readthedocs.io/en/stable/020_installation.html", Packages: map[string]string{"apt": "restic", "pacman": "restic", "dnf": "restic", "zypper": "restic", "winget": "Restic.Restic"}},
	{ID: "postgres-client", Title: "PostgreSQL client", Command: "pg_dump", Docs: "https://www.postgresql.org/docs/current/app-pgdump.html", Packages: map[string]string{"apt": "postgresql-client", "pacman": "postgresql", "dnf": "postgresql", "zypper": "postgresql"}},
	{ID: "mysql-client", Title: "MariaDB / MySQL client", Command: "mariadb-dump", Docs: "https://mariadb.com/kb/en/mariadb-dump/", Packages: map[string]string{"apt": "mariadb-client", "pacman": "mariadb-clients", "dnf": "mariadb", "zypper": "mariadb-client"}},
	{ID: "sqlite", Title: "SQLite", Command: "sqlite3", Docs: "https://sqlite.org/cli.html", Packages: map[string]string{"apt": "sqlite3", "pacman": "sqlite", "dnf": "sqlite", "zypper": "sqlite3", "winget": "SQLite.SQLite"}},
	{ID: "python3", Title: "Python 3", Command: "python3", Docs: "https://www.python.org/downloads/", Packages: map[string]string{"apt": "python3", "pacman": "python", "dnf": "python3", "zypper": "python3"}},
	{ID: "python-venv", Title: "Python virtual environments", Command: "", Docs: "https://docs.python.org/3/library/venv.html", Packages: map[string]string{"apt": "python3-venv"}},
}

type cliView struct {
	TerminalShell   string `json:"terminalShell,omitempty"`
	LauncherWarning string `json:"launcherWarning,omitempty"`
	ID              string `json:"id"`
	Title           string `json:"title"`
	Docs            string `json:"docs"`
	Command         string `json:"command"`
	Installed       bool   `json:"installed"`
	Managed         bool   `json:"managed"`
	Version         string `json:"version"`
	Package         string `json:"package"`
	CanInstall      bool   `json:"canInstall"`
	UserScope       bool   `json:"userScope"`
	TerminalCommand string `json:"terminalCommand"`
}

func cliByID(id string) (cliSpec, bool) {
	for _, s := range cliCatalog {
		if s.ID == id {
			return s, true
		}
	}
	return cliSpec{}, false
}

func cliRoot(create bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", fmt.Errorf("No account home directory is available")
	}
	base := home
	for _, part := range []string{".local", "share", "ervisio", "cli"} {
		if err := checkTrustedDir(base, os.Geteuid()); err != nil {
			return "", err
		}
		base = filepath.Join(base, part)
		if create {
			if err := os.Mkdir(base, 0700); err != nil && !os.IsExist(err) {
				return "", err
			}
		}
	}
	if err := checkTrustedDir(base, os.Geteuid()); err != nil {
		return "", err
	}
	return base, nil
}

func cliNode(root string) (string, string) {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "node", "node.exe"), filepath.Join(root, "node", "node_modules", "npm", "bin", "npm-cli.js")
	}
	return filepath.Join(root, "node", "bin", "node"), filepath.Join(root, "node", "lib", "node_modules", "npm", "bin", "npm-cli.js")
}

func cliPackage(root string, s cliSpec) string {
	base := filepath.Join(root, s.ID, "lib", "node_modules")
	if runtime.GOOS == "windows" {
		base = filepath.Join(root, s.ID, "node_modules")
	}
	return filepath.Join(base, filepath.FromSlash(s.NPM))
}

func cliManaged(root string, s cliSpec) (string, string) {
	if s.ID == "node-lts" {
		node, _ := cliNode(root)
		b, err := readOwnedFile(filepath.Join(root, "node", ".ervisio-version"), os.Geteuid())
		if err != nil {
			return "", ""
		}
		if fi, err := os.Stat(node); err == nil && fi.Mode().IsRegular() {
			return node, strings.TrimSpace(string(b))
		}
		return "", ""
	}
	if s.NPM == "" {
		return "", ""
	}
	pkg := cliPackage(root, s)
	return cliPackageInfo(pkg, s.Command)
}

func cliPackageInfo(pkg, command string) (string, string) {
	b, err := readOwnedFile(filepath.Join(pkg, "package.json"), os.Geteuid())
	if err != nil || len(b) > 1<<20 {
		return "", ""
	}
	var metadata struct {
		Version string          `json:"version"`
		Bin     json.RawMessage `json:"bin"`
	}
	if json.Unmarshal(b, &metadata) != nil {
		return "", ""
	}
	var bin string
	if json.Unmarshal(metadata.Bin, &bin) != nil {
		var bins map[string]string
		if json.Unmarshal(metadata.Bin, &bins) != nil {
			return "", ""
		}
		bin = bins[command]
	}
	if bin == "" || filepath.IsAbs(bin) || strings.Contains(bin, "\\") {
		return "", ""
	}
	entry := filepath.Join(pkg, filepath.FromSlash(bin))
	rel, err := filepath.Rel(pkg, entry)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", ""
	}
	if fi, err := os.Stat(entry); err != nil || !fi.Mode().IsRegular() {
		return "", ""
	}
	return entry, metadata.Version
}

func cliQuote(s string) string {
	if runtime.GOOS == "windows" {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
func cliTerminal(root, entry string) string {
	command := cliQuote(entry)
	if strings.HasSuffix(entry, ".js") || strings.HasSuffix(entry, ".mjs") {
		node, _ := cliNode(root)
		command = cliQuote(node) + " " + command
	}
	if runtime.GOOS == "windows" {
		command = "& " + command
	}
	return command
}

func cliList(ctx context.Context, c *rpc.Call) (any, error) {
	root, _ := cliRoot(false)
	manager := ""
	if primary := getManager().primary; primary != nil {
		manager = primary.Name()
	}
	views := make([]cliView, 0, len(cliCatalog))
	for _, s := range cliCatalog {
		if s.ID == "python-venv" && manager != "apt" {
			continue
		}
		v := cliView{ID: s.ID, Title: s.Title, Docs: s.Docs, Command: s.Command, Package: s.Packages[manager], UserScope: s.NPM != "" || s.ID == "node-lts"}
		if s.ID == "python-venv" {
			check, cancel := context.WithTimeout(ctx, 2*time.Second)
			_, err := (sys.Cmd{Name: "python3", Args: []string{"-I", "-c", "import venv, ensurepip"}, MaxOutput: 1024}).Output(check)
			cancel()
			v.Installed = err == nil
		}
		if root != "" {
			if entry, version := cliManaged(root, s); entry != "" {
				v.Installed = true
				v.Managed = true
				v.Version = version
				v.TerminalCommand = cliTerminal(root, entry)
				v.LauncherWarning = cliLauncherWarning(root, s)
			}
		}
		if !v.Installed {
			commands := []string{s.Command}
			if s.ID == "mysql-client" {
				commands = append(commands, "mysqldump")
			}
			for _, command := range commands {
				if command == "" {
					continue
				}
				if executable, err := cliFindExternal(command); err == nil {
					v.Installed = true
					v.TerminalCommand = cliQuote(executable)
					if runtime.GOOS == "windows" {
						v.TerminalCommand = "& " + v.TerminalCommand
					}
					if s.NPM == "" {
						check, cancel := context.WithTimeout(ctx, 2*time.Second)
						out, err := (sys.Cmd{Name: executable, Args: []string{"--version"}, MaxOutput: 2048}).Output(check)
						cancel()
						if err == nil {
							v.Version = strings.Split(strings.TrimSpace(string(out)), "\n")[0]
						}
					}
					break
				}
			}
		}
		v.CanInstall = (v.UserScope && !c.Admin && !isRoot() && (runtime.GOOS == "linux" || runtime.GOOS == "windows") && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")) || v.Package != ""
		if runtime.GOOS == "windows" {
			v.TerminalShell = cliPowerShell()
			if v.TerminalShell == "" {
				v.TerminalCommand = ""
			}
		}
		views = append(views, v)
	}
	return views, nil
}

func cliPowerShell() string {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if executable, err := sys.LookPath(name); err == nil {
			return executable
		}
	}
	root := os.Getenv("SYSTEMROOT")
	if root == "" {
		root = `C:\Windows`
	}
	candidate := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if fi, err := os.Stat(candidate); err == nil && fi.Mode().IsRegular() {
		return candidate
	}
	return ""
}

func cliFindExternal(command string) (string, error) {
	if executable, err := sys.LookPath(command); err == nil {
		return executable, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, dir := range []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, ".npm-global", "bin"), filepath.Join(os.Getenv("APPDATA"), "npm")} {
		if !filepath.IsAbs(dir) || checkTrustedDir(dir, os.Geteuid()) != nil {
			continue
		}
		if executable, err := sys.LookPath(filepath.Join(dir, command)); err == nil {
			return executable, nil
		}
	}
	return "", fmt.Errorf("Command is not installed for this account")
}

func cliHistory() []HistoryEntry {
	root, err := cliRoot(false)
	if err != nil {
		return nil
	}
	b, err := readOwnedFile(filepath.Join(root, "history.json"), os.Geteuid())
	if err != nil {
		return nil
	}
	var entries []HistoryEntry
	_ = json.Unmarshal(b, &entries)
	if len(entries) > 200 {
		entries = entries[len(entries)-200:]
	}
	return entries
}
func cliRecord(root string, p TxParams) {
	entries := cliHistory()
	action := map[string]string{"install": "installed", "upgrade": "upgraded", "remove": "removed"}[p.Op]
	now := time.Now().UnixMilli()
	for _, id := range p.Packages {
		s, _ := cliByID(id)
		_, version := cliManaged(root, s)
		entries = append(entries, HistoryEntry{Time: now, Tx: int(now), Name: s.Title, Action: action, To: version})
	}
	if len(entries) > 200 {
		entries = entries[len(entries)-200:]
	}
	b, _ := json.Marshal(entries)
	_ = writeFileAtomic(filepath.Join(root, "history.json"), b, 0600)
}
func mergeCLIHistory(entries []HistoryEntry, limit int) []HistoryEntry {
	entries = append(entries, cliHistory()...)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Time > entries[j].Time })
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}
