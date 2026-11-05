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
	"path/filepath"
	"runtime"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/config"
	"github.com/ervisio/ervisio/server/internal/legacy"
	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
	"github.com/ervisio/ervisio/server/internal/modules/plugins"
	"github.com/ervisio/ervisio/server/internal/notify"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/server"
	"github.com/ervisio/ervisio/server/internal/update"
)

// hiddenFlags are omitted from -help.
var hiddenFlags = map[string]bool{"dev-insecure-noauth": true}

func main() {
	// A Windows service gets its stop request from the service manager
	// (service_windows.go); everywhere else from signals.
	if runAsService(run) {
		return
	}
	ctx, stop := consoleContext()
	defer stop()
	run(ctx)
}

// run is the daemon: it serves until ctx is cancelled (a signal in console
// mode, a Stop/Shutdown request when running as a Windows service).
func run(ctx context.Context) {
	runInternalMode()
	f := parseFlags()
	if f.version {
		fmt.Println(brand.Version)
		return
	}
	if f.check {
		path := f.configPath
		if flag.NArg() > 0 {
			path = flag.Arg(0)
		}
		os.Exit(checkConfig(path))
	}
	log.SetFlags(log.LstdFlags)
	log.SetPrefix(brand.DaemonBinary + ": ")

	if f.noAuth && !f.dev {
		log.Fatal("--dev-insecure-noauth requires --dev")
	}
	if f.noAuth && bridge.RunningAsServiceIdentity() {
		log.Fatal("--dev-insecure-noauth refuses to run as root")
	}
	// A machine that ran LinuxAdmin: its configuration, certificate, plugins
	// and state are copied to the Ervisio locations before they are read
	// (once; a no-op afterwards). See internal/legacy.
	if !f.dev && bridge.Privileged() && f.configPath == brand.ConfigPath {
		importLegacyData()
	}
	cfgAbs, err := filepath.Abs(f.configPath)
	if err != nil {
		log.Fatal(err)
	}
	bp := resolveBridgePath(f.bridgePath)
	// A versioned install (/usr/lib/ervisio/versions/<v>/bin) serves the
	// web app and packaged plugins of its own version.
	versionDir, versioned := update.RunningVersionDir()
	webDir := resolveWebDir(f.web, f.dev, versionDir, versioned)
	pluginDirs := []string{brand.PackagedPluginsDir, brand.InstalledPluginsDir}
	if versioned {
		pluginDirs[0] = filepath.Join(versionDir, "plugins")
	}
	// Plugins work in this process (as root) uses the same configuration
	// and packaged folder as the bridges.
	configmod.Path = cfgAbs
	plugins.SystemDir = pluginDirs[0]
	if f.installPlugin != "" || f.skipMoved {
		os.Exit(pluginCommand(f.installPlugin, f.skipMoved))
	}
	devPlugins := ""
	if f.dev {
		devPlugins, pluginDirs = setupDevPlugins(pluginDirs)
	}
	state := resolveStatePaths(f.devState, f.dev)

	helper := ""
	if !f.dev && runtime.GOOS != "windows" {
		helper = sessionHelperPath()
	}

	srv, err := server.New(server.Options{
		ConfigPath:        cfgAbs,
		Dev:               f.dev,
		NoAuth:            f.noAuth,
		Listen:            f.listen,
		WebDir:            webDir,
		ViteURL:           f.vite,
		Bridge:            bp,
		PluginDirs:        pluginDirs,
		DevPluginsDir:     devPlugins,
		DevPluginsFile:    state.devPluginsFile,
		SessionHelper:     helper,
		DevAuthorizedKeys: f.devKeys,
		StateDir:          state.dir,
		NotifyFile:        state.notifyFile,
		EnvsDir:           f.envsDir,
		TunnelDir:         f.tunnelDir,
		Logger:            log.Default(),
	})
	if err != nil {
		log.Fatal(err)
	}
	if !f.dev {
		startUpdates(ctx, srv)
	}
	if !f.dev && bridge.Privileged() {
		startMovedPluginsMigration(ctx)
	}
	if err := srv.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

// runInternalMode exits the process when the arguments ask for one of the
// helper modes the daemon binary also serves; otherwise it returns.
func runInternalMode() {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[1]
	// Internal mode: the root helper that opens a PAM session around a user
	// bridge (started by the daemon itself, never by hand).
	if mode == bridge.HelperFlag {
		os.Exit(bridge.RunSessionHelper(os.Args[2:]))
	}
	// Internal mode: switch to another installed version, restart the
	// service and roll back if it does not answer (started by updates.apply
	// in a transient systemd unit, never by hand).
	if mode == update.HelperFlag {
		// Started by LinuxAdmin's updater from its own layout: this release
		// moves the installation to Ervisio instead of a plain switch.
		if _, ok := legacyExe(); ok {
			os.Exit(legacy.RunHelper(os.Args[2:]))
		}
		os.Exit(update.RunHelper(os.Args[2:]))
	}
	// Internal mode: move a LinuxAdmin installation to Ervisio (started in a
	// transient unit by a daemon that runs from LinuxAdmin's layout).
	if mode == legacy.TransitionFlag {
		os.Exit(legacy.RunHelper(os.Args[2:]))
	}
	// Installer and package scripts: import LinuxAdmin's data, or remove
	// what is left of it.
	if mode == legacy.MigrateFlag || mode == legacy.RemoveFlag {
		os.Exit(legacy.RunMigrate(mode, os.Args[2:]))
	}
}

type daemonFlags struct {
	configPath    string
	dev           bool
	listen        string
	web           string
	vite          string
	bridgePath    string
	noAuth        bool
	devKeys       string
	devState      string
	envsDir       string
	tunnelDir     string
	check         bool
	version       bool
	installPlugin string
	skipMoved     bool
}

func parseFlags() daemonFlags {
	configPath := flag.String("config", brand.ConfigPath, "configuration file")
	dev := flag.Bool("dev", false, "development mode: plain HTTP on 127.0.0.1:9090, no root, only your own user")
	listen := flag.String("listen", "", "listen address (overrides the config; dev default 127.0.0.1:9090)")
	web := flag.String("web", "", "directory of the built web app (default "+brand.WebDir+"; in dev, empty = proxy to Vite)")
	vite := flag.String("vite", "http://127.0.0.1:5173", "Vite dev server to proxy to in --dev without --web")
	bridgePath := flag.String("bridge", "", "path of "+brand.BridgeBinary+" (default: next to this binary)")
	noAuth := flag.Bool("dev-insecure-noauth", false, "dev only: sign every request in as the daemon's user")
	devKeys := flag.String("dev-authorized-keys", "", "dev only: authorized_keys file used for SSH-key sign-in instead of ~/.ssh")
	devState := flag.String("dev-state-dir", "", "dev only: folder for the daemon's state (plugin jobs, notification channels) instead of "+brand.StateDir)
	envsDir := flag.String("envs-dir", "", "where environments (remote Docker hosts) and their secrets are stored (default "+brand.StateDir+"/envs)")
	tunnelDir := flag.String("tunnel-dir", "", "where per-user tunnel sockets to environments are created (default "+brand.RunDir+"/tunnels)")
	check := flag.Bool("check-config", false, "parse and validate the configuration (the file after the flag, else -config), print OK or the errors, exit 0 or 1; starts nothing")
	version := flag.Bool("version", false, "print the version and exit")
	installPlugin := flag.String("install-plugin", "", "install (or update) this plugin from the signed marketplace catalog, then exit (root)")
	skipMoved := flag.Bool("skip-moved-plugins", false, "do not install the plugins that left "+brand.Name+" (Docker) from the marketplace on start, then exit (root)")
	flag.Usage = printUsage
	flag.Parse()
	return daemonFlags{
		configPath:    *configPath,
		dev:           *dev,
		listen:        *listen,
		web:           *web,
		vite:          *vite,
		bridgePath:    *bridgePath,
		noAuth:        *noAuth,
		devKeys:       *devKeys,
		devState:      *devState,
		envsDir:       *envsDir,
		tunnelDir:     *tunnelDir,
		check:         *check,
		version:       *version,
		installPlugin: *installPlugin,
		skipMoved:     *skipMoved,
	}
}

func printUsage() {
	_, _ = fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags]\n", brand.DaemonBinary)
	flag.VisitAll(func(f *flag.Flag) {
		if hiddenFlags[f.Name] {
			return
		}
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "  -%s\n    \t%s\n", f.Name, f.Usage)
	})
}

func importLegacyData() {
	res, err := legacy.ImportOnStart(legacy.Paths{}, log.Printf)
	if err != nil {
		log.Printf("copy %s's data: %v", brand.LegacyName, err)
		return
	}
	if res.Any() {
		log.Printf("%s's data was copied to the %s locations (see %s/%s)", brand.LegacyName, brand.Name, brand.LegacyConfigDir, legacy.NoteFile)
	}
}

// resolveBridgePath returns the absolute, symlink-free path of the bridge
// binary: the flag value, else the one installed next to this daemon.
func resolveBridgePath(flagPath string) string {
	bp := flagPath
	if bp == "" {
		bp = defaultBridgePath()
	}
	bp, err := filepath.Abs(bp)
	if err != nil {
		log.Fatal(err)
	}
	// Pin the bridge of this version: with the versioned layout the path
	// may go through the `current` symlink, which an update moves.
	if r, err := filepath.EvalSymlinks(bp); err == nil {
		bp = r
	}
	return bp
}

func defaultBridgePath() string {
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	bp := filepath.Join(filepath.Dir(exe), brand.BridgeBinary+exeSuffix)
	// Packages (flat layout) install the daemon as /usr/bin/ervisiod
	// and the bridge as /usr/lib/ervisio/ervisio-bridge.
	if _, err := os.Stat(bp); err == nil {
		return bp
	}
	if alt := filepath.Join(brand.LibDir, brand.BridgeBinary); isFile(alt) {
		return alt
	}
	if alt := filepath.Join(filepath.Dir(exe), brand.LegacyBridgeBinary); isFile(alt) {
		// Started from LinuxAdmin's layout (compatibility archive).
		return alt
	}
	return bp
}

func resolveWebDir(flagDir string, dev bool, versionDir string, versioned bool) string {
	webDir := flagDir
	if webDir == "" && !dev {
		webDir = brand.WebDir
		if versioned {
			webDir = filepath.Join(versionDir, "web")
		}
	}
	if webDir == "" {
		return ""
	}
	abs, err := filepath.Abs(webDir)
	if err != nil {
		log.Fatal(err)
	}
	return abs
}

// setupDevPlugins puts the working directory's plugins folder first and
// lets the daemon itself resolve plugins for background jobs and
// notifications, as the bridges do (--dev, --dev-plugins).
func setupDevPlugins(pluginDirs []string) (string, []string) {
	devPlugins := ""
	if wd, err := os.Getwd(); err == nil {
		devPlugins = filepath.Join(wd, "plugins")
		pluginDirs = append([]string{devPlugins}, pluginDirs...)
	}
	plugins.DaemonDev = true
	plugins.DaemonPluginsDir = devPlugins
	return devPlugins, pluginDirs
}

type statePaths struct {
	dir            string
	notifyFile     string
	devPluginsFile string
}

func resolveStatePaths(devState string, dev bool) statePaths {
	if devState != "" {
		if !dev {
			log.Fatal("--dev-state-dir requires --dev")
		}
		dir, err := filepath.Abs(devState)
		if err != nil {
			log.Fatal(err)
		}
		devPluginsFile := filepath.Join(dir, "plugins-dev.json")
		plugins.DevFile = devPluginsFile
		return statePaths{
			dir:            dir,
			notifyFile:     filepath.Join(dir, "notify.json"),
			devPluginsFile: devPluginsFile,
		}
	}
	if dev {
		dir := filepath.Join(os.TempDir(), fmt.Sprintf("ervisio-dev-state-%d", os.Getuid()))
		return statePaths{dir: dir, notifyFile: filepath.Join(dir, "notify.json")}
	}
	return statePaths{}
}

// sessionHelperPath returns the daemon's own binary, which serves as the PAM
// session helper. Windows has no PAM session: the bridge starts directly
// with the user's token.
func sessionHelperPath() string {
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	helper, err := filepath.EvalSymlinks(exe)
	if err != nil {
		log.Fatal(err)
	}
	return helper
}

// startUpdates runs automatic update checks and installs ([updates] in the
// config), and moves a LinuxAdmin-layout installation to Ervisio.
func startUpdates(ctx context.Context, srv *server.Server) {
	u := update.New(update.NewChecker())
	u.ConfigPath = configmod.Path
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
		startAutoUpdate(ctx, srv, u)
	}
	// Started by linuxadmin.service from LinuxAdmin's layout: move to
	// ervisio.service. The transition stops this process.
	if v, ok := legacyExe(); ok && bridge.Privileged() {
		go startLegacyTransition(ctx, exe, v)
	}
}

func startAutoUpdate(ctx context.Context, srv *server.Server, u *update.Updater) {
	auto := &update.Auto{Updater: u, Config: srv.Config, Logf: log.Printf}
	// A new release also goes to the notification channels that
	// subscribe to "updates" (Settings > Notification channels).
	auto.Notify = func(version, current string) {
		srv.NotifyCore(notify.Message{
			Title:  fmt.Sprintf("%s %s is available", brand.Name, version),
			Body:   fmt.Sprintf("This server runs %s. Update it in Settings > About.", current),
			Level:  "info",
			Source: brand.Name,
			Link:   "/settings#about",
		}, notify.EventUpdates)
	}
	go auto.Run(ctx)
}

func startLegacyTransition(ctx context.Context, exe, legacyVersion string) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(3 * time.Second):
	}
	log.Printf("running from %s's layout: moving this installation to %s", brand.LegacyName, brand.Name)
	if err := legacy.StartTransition(ctx, legacy.Paths{}, exe, legacyVersion, update.SystemdRun); err != nil {
		log.Printf("not moving to %s: %v", brand.Name, err)
	}
}

// startMovedPluginsMigration keeps the plugins that moved out of the core
// (Docker) on machines that use them (internal/modules/plugins/moved.go).
func startMovedPluginsMigration(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
		plugins.MigrateMoved(ctx, log.Printf)
	}()
}

// pluginCommand implements --install-plugin and --skip-moved-plugins.
func pluginCommand(id string, skip bool) int {
	if !bridge.Privileged() {
		fmt.Fprintln(os.Stderr, "run this as root")
		return 1
	}
	if skip {
		if err := skipMovedPlugins(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if id == "" {
			return 0
		}
	}
	return installPlugin(id)
}

// skipMovedPlugins marks every moved plugin without a decision as skipped.
func skipMovedPlugins() error {
	for _, mp := range plugins.Moved {
		if plugins.ReadMoved()[mp.ID] != "" {
			continue
		}
		if err := plugins.SetMoved(mp.ID, plugins.MovedSkipped); err != nil {
			return err
		}
	}
	return nil
}

// installPlugin installs a catalog plugin and reports the result.
func installPlugin(id string) int {
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
