package software

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

func validateCLITx(p TxParams, admin bool) error {
	if admin || isRoot() {
		return rpc.Errorf(rpc.Invalid, "Managed CLI tools must be installed from a normal user account, without administrator unlock.")
	}
	if p.Op != "install" && p.Op != "remove" && p.Op != "upgrade" {
		return rpc.Errorf(rpc.Invalid, "Unknown CLI operation")
	}
	if len(p.Packages) != 1 {
		return rpc.Errorf(rpc.Invalid, "Choose one managed CLI tool")
	}
	s, ok := cliByID(p.Packages[0])
	if !ok || (s.NPM == "" && s.ID != "node-lts") {
		return rpc.Errorf(rpc.Invalid, "Unknown managed CLI tool")
	}
	return nil
}

func runCLITransaction(ctx context.Context, c *rpc.Call, stream rpc.Stream, p TxParams) error {
	if err := validateCLITx(p, c.Admin); err != nil {
		return err
	}
	root, err := cliRoot(true)
	if err != nil {
		return err
	}
	if err := acquireTx(&manager{}); err != nil {
		return err
	}
	unlock, err := lockCLI(root)
	if err != nil {
		releaseTx()
		return rpc.Errorf(rpc.Conflict, "%v", err)
	}
	sender := &streamSender{stream: stream, alive: true}
	full := filepath.Join(root, "transaction.json")
	run := &txRun{path: full, send: sender.send}
	startTransaction(run, p, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer releaseTx()
		defer unlock()
		job, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		s, _ := cliByID(p.Packages[0])
		run.mu.Lock()
		run.st.Step = 1
		run.st.StepTitle = s.Title
		run.save(true)
		run.mu.Unlock()
		run.send(map[string]any{"type": "step", "index": 1, "title": s.Title, "command": p.Op + " " + s.Title + " (current account)"})
		err := changeCLI(job, root, s, p.Op, run)
		message := ""
		if err != nil {
			message = err.Error()
			run.addLog(message)
		} else {
			cliRecord(root, p)
		}
		recordOutcome(run, err == nil, message, false, false)
		run.send(map[string]any{"type": "done", "ok": err == nil, "message": message, "hint": "", "rebootNeeded": false})
	}()
	select {
	case <-finished:
	case <-ctx.Done():
		sender.stop()
	}
	return nil
}

func changeCLI(ctx context.Context, root string, s cliSpec, op string, run *txRun) error {
	if op == "remove" {
		if s.ID == "node-lts" {
			for _, other := range cliCatalog {
				if other.NPM != "" {
					if entry, _ := cliManaged(root, other); entry != "" {
						return fmt.Errorf("Remove managed %s before removing its Node runtime", other.Title)
					}
				}
			}
		}
		target := filepath.Join(root, s.ID)
		if s.ID == "node-lts" {
			target = filepath.Join(root, "node")
		}
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			return rpc.Errorf(rpc.Invalid, "This CLI is not managed by Ervisio; remove it with its original package manager")
		}
		if err := checkTrustedDir(target, os.Geteuid()); err != nil {
			return err
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		removeCLILauncher(root, s)
		if s.ID == "node-lts" {
			for _, command := range []string{"npm", "npx"} {
				removeCLILauncher(root, cliSpec{ID: s.ID, Command: command})
			}
		}
		if _, err := os.Lstat(target + ".previous"); err == nil {
			if err := checkTrustedDir(target+".previous", os.Geteuid()); err != nil {
				return err
			}
			if err := os.RemoveAll(target + ".previous"); err != nil {
				return err
			}
		}
		run.addLog("Removed managed program files; account credentials and project files were kept")
		return nil
	}
	if s.ID == "node-lts" {
		if err := installNode(ctx, root, run.addLog); err != nil {
			return err
		}
		if err := ensureCLILauncher(root, s); err != nil {
			run.addLog(err.Error())
		}
		for _, command := range []string{"npm", "npx"} {
			if err := ensureCLILauncher(root, cliSpec{ID: s.ID, Command: command}); err != nil {
				run.addLog(err.Error())
			}
		}
		return nil
	}
	node, npm := cliNode(root)
	if _, err := os.Stat(node); err != nil {
		return rpc.Errorf(rpc.Unavailable, "Install the managed Node.js LTS runtime first. No system packages were changed.")
	}
	if _, err := os.Stat(npm); err != nil {
		return rpc.Errorf(rpc.Unavailable, "Managed npm is missing; reinstall Node.js LTS")
	}
	stage, err := os.MkdirTemp(root, "."+s.ID+"-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	config := filepath.Join(stage, "empty.npmrc")
	if err := os.WriteFile(config, nil, 0600); err != nil {
		return err
	}
	step := Step{Title: s.Title, Name: node, Args: []string{npm, "install", "--global", "--prefix", stage, "--userconfig", config, "--registry", "https://registry.npmjs.org", "--no-audit", "--no-fund", "--include=optional", s.NPM + "@latest"}, Env: []string{"PATH=" + filepath.Dir(node) + string(os.PathListSeparator) + sys.SafePath, "NPM_CONFIG_CACHE=" + filepath.Join(root, "npm-cache"), "NPM_CONFIG_UPDATE_NOTIFIER=false"}}
	if err := runStep(ctx, step, run); err != nil {
		return err
	}
	packageRoot := filepath.Join(stage, "lib", "node_modules", filepath.FromSlash(s.NPM))
	if filepath.Base(node) == "node.exe" {
		packageRoot = filepath.Join(stage, "node_modules", filepath.FromSlash(s.NPM))
	}
	if entry, _ := cliPackageInfo(packageRoot, s.Command); entry == "" {
		return fmt.Errorf("%s did not provide a usable executable; previous installation was preserved", s.Title)
	}
	if err := adoptCLI(stage, filepath.Join(root, s.ID)); err != nil {
		return err
	}
	if entry, _ := cliManaged(root, s); entry == "" {
		if err := rollbackCLI(filepath.Join(root, s.ID)); err != nil {
			return fmt.Errorf("%s executable validation failed and previous installation could not be restored: %w", s.Title, err)
		}
		return fmt.Errorf("%s executable validation failed; previous installation restored", s.Title)
	}
	if err := ensureCLILauncher(root, s); err != nil {
		run.addLog(err.Error())
	}
	return nil
}
