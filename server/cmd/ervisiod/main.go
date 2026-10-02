// Command ervisiod is the Ervisio web console daemon. It runs as
// root, signs users in with PAM, and routes their requests to per-user
// ervisio-bridge processes. See docs/ARCHITECTURE.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/config"
	"github.com/ervisio/ervisio/server/internal/legacy"
	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
	"github.com/ervisio/ervisio/server/internal/modules/plugins"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/server"
	"github.com/ervisio/ervisio/server/internal/update"
)

// hiddenFlags are omitted from -help.
var hiddenFlags = map[string]bool{"dev-insecure-noauth": true}

func main() {
	// Internal mode: the root helper that opens a PAM session around a user
	// bridge (started by the daemon itself, never by hand).
	if len(os.Args) > 1 && os.Args[1] == bridge.HelperFlag {
		os.Exit(bridge.RunSessionHelper(os.Args[2:]))
	}
	// Internal mode: switch to another installed version, restart the
	// service and roll back if it does not answer (started by updates.apply
	// in a transient systemd unit, never by hand).
	if len(os.Args) > 1 && os.Args[1] == update.HelperFlag {
		// Started by LinuxAdmin's updater from its own layout: this release
		// moves the installation to Ervisio instead of a plain switch.
		if _, ok := legacyExe(); ok {
			os.Exit(legacy.RunHelper(os.Args[2:]))
		}
		os.Exit(update.RunHelper(os.Args[2:]))
	}
	// Internal mode: move a LinuxAdmin installation to Ervisio (started in a
	// transient unit by a daemon that runs from LinuxAdmin's layout).
	if len(os.Args) > 1 && os.Args[1] == legacy.TransitionFlag {
		os.Exit(legacy.RunHelper(os.Args[2:]))
	}
	// Installer and package scripts: import LinuxAdmin's data, or remove
	// what is left of it.
	if len(os.Args) > 1 && (os.Args[1] == legacy.MigrateFlag || os.Args[1] == legacy.RemoveFlag) {
		os.Exit(legacy.RunMigrate(os.Args[1], os.Args[2:]))
	}
	configPath := flag.String("config", brand.ConfigPath, "configuration file")
	dev := flag.Bool("dev", false, "development mode: plain HTTP on 127.0.0.1:9090, no root, only your own user")
	listen := flag.String("listen", "", "listen address (overrides the config; dev default 127.0.0.1:9090)")
	web := flag.String("web", "", "directory of the built web app (default "+brand.WebDir+"; in dev, empty = proxy to Vite)")
	vite := flag.String("vite", "http://127.0.0.1:5173", "Vite dev server to proxy to in --dev without --web")
	bridgePath := flag.String("bridge", "", "path of "+brand.BridgeBinary+" (default: next to this binary)")
	noAuth := flag.Bool("dev-insecure-noauth", false, "dev only: sign every request in as the daemon's user")
	devKeys := flag.String("dev-authorized-keys", "", "dev only: authorized_keys file used for SSH-key sign-in instead of ~/.ssh")
	envsDir := flag.String("envs-dir", "", "where environments (remote Docker hosts) and their secrets are stored (default "+brand.StateDir+"/envs)")
	tunnelDir := flag.String("tunnel-dir", "", "where per-user tunnel sockets to environments are created (default "+brand.RunDir+"/tunnels)")
	check := flag.Bool("check-config", false, "parse and validate the configuration (the file after the flag, else -config), print OK or the errors, exit 0 or 1; starts nothing")
	version := flag.Bool("version", false, "print the version and exit")
	installPlugin := flag.String("install-plugin", "", "install (or update) this plugin from the signed marketplace catalog, then exit (root)")
	skipMoved := flag.Bool("skip-moved-plugins", false, "do not install the plugins that left "+brand.Name+" (Docker) from the marketplace on start, then exit (root)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags]\n", brand.DaemonBinary)
		flag.VisitAll(func(f *flag.Flag) {
			if hiddenFlags[f.Name] {
				return
			}
			fmt.Fprintf(flag.CommandLine.Output(), "  -%s\n    \t%s\n", f.Name, f.Usage)
		})
	}
	flag.Parse()
	if *version {
		fmt.Println(brand.Version)
		return
	}
	if *check {
		path := *configPath
		if flag.NArg() > 0 {
			path = flag.Arg(0)
		}
		os.Exit(checkConfig(path))
	}
	log.SetFlags(log.LstdFlags)
	log.SetPrefix(brand.DaemonBinary + ": ")

	if *noAuth && !*dev {
		log.Fatal("--dev-insecure-noauth requires --dev")
	}
	if *noAuth && os.Geteuid() == 0 {
		log.Fatal("--dev-insecure-noauth refuses to run as root")
	}
	// A machine that ran LinuxAdmin: its configuration, certificate, plugins
	// and state are copied to the Ervisio locations before they are read
	// (once; a no-op afterwards). See internal/legacy.
	if !*dev && os.Geteuid() == 0 && *configPath == brand.ConfigPath {
		if res, err := legacy.ImportOnStart(legacy.Paths{}, log.Printf); err != nil {
			log.Printf("copy %s's data: %v", brand.LegacyName, err)
		} else if res.Any() {
			log.Printf("%s's data was copied to the %s locations (see %s/%s)", brand.LegacyName, brand.Name, brand.LegacyConfigDir, legacy.NoteFile)
		}
	}
	cfgAbs, err := filepath.Abs(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	bp := *bridgePath
	if bp == "" {
		exe, err := os.Executable()
		if err != nil {
			log.Fatal(err)
		}
		bp = filepath.Join(filepath.Dir(exe), brand.BridgeBinary)
		// Packages (flat layout) install the daemon as /usr/bin/ervisiod
		// and the bridge as /usr/lib/ervisio/ervisio-bridge.
		if _, err := os.Stat(bp); err != nil {
			if alt := filepath.Join(brand.LibDir, brand.BridgeBinary); isFile(alt) {
				bp = alt
			} else if alt := filepath.Join(filepath.Dir(exe), brand.LegacyBridgeBinary); isFile(alt) {
				// Started from LinuxAdmin's layout (compatibility archive).
				bp = alt
			}
		}
	}
	if bp, err = filepath.Abs(bp); err != nil {
		log.Fatal(err)
	}
	// Pin the bridge of this version: with the versioned layout the path
	// may go through the `current` symlink, which an update moves.
	if r, err := filepath.EvalSymlinks(bp); err == nil {
		bp = r
	}
	// A versioned install (/usr/lib/ervisio/versions/<v>/bin) serves the
	// web app and packaged plugins of its own version.
	versionDir, versioned := update.RunningVersionDir()
	webDir := *web
	if webDir == "" && !*dev {
		webDir = brand.WebDir
		if versioned {
			webDir = filepath.Join(versionDir, "web")
		}
	}
	if webDir != "" {
		if webDir, err = filepath.Abs(webDir); err != nil {
			log.Fatal(err)
		}
	}
	pluginDirs := []string{brand.PackagedPluginsDir, brand.InstalledPluginsDir}
	if versioned {
		pluginDirs[0] = filepath.Join(versionDir, "plugins")
	}
	// Plugins work in this process (as root) uses the same configuration
	// and packaged folder as the bridges.
	configmod.Path = cfgAbs
	plugins.SystemDir = pluginDirs[0]
	if *installPlugin != "" || *skipMoved {
		os.Exit(pluginCommand(*installPlugin, *skipMoved))
	}
	devPlugins := ""
	if *dev {
		if wd, err := os.Getwd(); err == nil {
			devPlugins = filepath.Join(wd, "plugins")
			pluginDirs = append([]string{devPlugins}, pluginDirs...)
		}
	}

	helper := ""
	if !*dev {
		// The daemon's own binary serves as the PAM session helper.
		exe, err := os.Executable()
		if err != nil {
			log.Fatal(err)
		}
		if helper, err = filepath.EvalSymlinks(exe); err != nil {
			log.Fatal(err)
		}
	}

	srv, err := server.New(server.Options{
		ConfigPath:        cfgAbs,
		Dev:               *dev,
		NoAuth:            *noAuth,
		Listen:            *listen,
		WebDir:            webDir,
		ViteURL:           *vite,
		Bridge:            bp,
		PluginDirs:        pluginDirs,
		DevPluginsDir:     devPlugins,
		SessionHelper:     helper,
		DevAuthorizedKeys: *devKeys,
		EnvsDir:           *envsDir,
		TunnelDir:         *tunnelDir,
		Logger:            log.Default(),
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	signal.Ignore(syscall.SIGPIPE)
	if !*dev {
		// Automatic update checks and installs ([updates] in the config).
		u := update.New(update.NewChecker())
		exe, _ := os.Executable()
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		ok, why := u.Supported(exe, false)
		if !ok {
			log.Printf("self-update disabled: %s", why)
		}
		// A packaged install still checks (and logs) new releases; Auto
		// never installs them there.
		if ok || u.Layout.ManagedBy() != "" {
			auto := &update.Auto{Updater: u, Config: srv.Config, Logf: log.Printf}
			go auto.Run(ctx)
		}
		// Started by linuxadmin.service from LinuxAdmin's layout: move to
		// ervisio.service. The transition stops this process.
		if v, ok := legacyExe(); ok && os.Geteuid() == 0 {
			go func() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(3 * time.Second):
				}
				log.Printf("running from %s's layout: moving this installation to %s", brand.LegacyName, brand.Name)
				if err := legacy.StartTransition(ctx, legacy.Paths{}, exe, v, update.SystemdRun); err != nil {
					log.Printf("not moving to %s: %v", brand.Name, err)
				}
			}()
		}
	}
	if !*dev && os.Geteuid() == 0 {
		// Plugins that moved out of the core (Docker): keep them on machines
		// that use them (internal/modules/plugins/moved.go).
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			plugins.MigrateMoved(ctx, log.Printf)
		}()
	}
	if err := srv.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

// pluginCommand implements --install-plugin and --skip-moved-plugins.
func pluginCommand(id string, skip bool) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "run this as root")
		return 1
	}
	if skip {
		for _, mp := range plugins.Moved {
			if plugins.ReadMoved()[mp.ID] == "" {
				if err := plugins.SetMoved(mp.ID, plugins.MovedSkipped); err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 1
				}
			}
		}
		if id == "" {
			return 0
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	in, err := plugins.InstallFromCatalog(ctx, id)
	if err != nil {
		var re *rpc.Error
		if errors.As(err, &re) {
			err = errors.New(re.Message)
		}
		fmt.Fprintf(os.Stderr, "could not install the plugin %s: %v\n", id, err)
		return 1
	}
	for _, mp := range plugins.Moved {
		if mp.ID == id {
			_ = plugins.SetMoved(id, plugins.MovedInstalled)
		}
	}
	fmt.Printf("installed %s %s (signature verified) in %s\n", in.Name, in.Version, plugins.InstalledDir)
	return 0
}

// checkConfig implements --check-config: 0 when the file is valid.
func checkConfig(path string) int {
	warn, err := config.Check(path)
	for _, w := range warn {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: invalid configuration:\n%v\n", path, err)
		return 1
	}
	fmt.Printf("OK: %s\n", path)
	return 0
}

// legacyExe returns the version folder when this binary runs from
// LinuxAdmin's versioned layout (/usr/lib/linuxadmin/versions/<v>/bin).
func legacyExe() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", false
	}
	return legacy.LegacyVersionOf(legacy.Paths{}, exe)
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
