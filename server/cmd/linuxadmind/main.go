// Command linuxadmind is the LinuxAdmin web console daemon. It runs as
// root, signs users in with PAM, and routes their requests to per-user
// linuxadmin-bridge processes. See docs/ARCHITECTURE.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Fonlogen/LinuxAdmin/server/internal/brand"
	"github.com/Fonlogen/LinuxAdmin/server/internal/bridge"
	"github.com/Fonlogen/LinuxAdmin/server/internal/server"
)

// hiddenFlags are omitted from -help.
var hiddenFlags = map[string]bool{"dev-insecure-noauth": true}

func main() {
	// Internal mode: the root helper that opens a PAM session around a user
	// bridge (started by the daemon itself, never by hand).
	if len(os.Args) > 1 && os.Args[1] == bridge.HelperFlag {
		os.Exit(bridge.RunSessionHelper(os.Args[2:]))
	}
	configPath := flag.String("config", brand.ConfigPath, "configuration file")
	dev := flag.Bool("dev", false, "development mode: plain HTTP on 127.0.0.1:9090, no root, only your own user")
	listen := flag.String("listen", "", "listen address (overrides the config; dev default 127.0.0.1:9090)")
	web := flag.String("web", "", "directory of the built web app (default "+brand.WebDir+"; in dev, empty = proxy to Vite)")
	vite := flag.String("vite", "http://127.0.0.1:5173", "Vite dev server to proxy to in --dev without --web")
	bridgePath := flag.String("bridge", "", "path of "+brand.BridgeBinary+" (default: next to this binary)")
	noAuth := flag.Bool("dev-insecure-noauth", false, "dev only: sign every request in as the daemon's user")
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
	log.SetFlags(log.LstdFlags)
	log.SetPrefix(brand.DaemonBinary + ": ")

	if *noAuth && !*dev {
		log.Fatal("--dev-insecure-noauth requires --dev")
	}
	if *noAuth && os.Geteuid() == 0 {
		log.Fatal("--dev-insecure-noauth refuses to run as root")
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
	}
	if bp, err = filepath.Abs(bp); err != nil {
		log.Fatal(err)
	}
	webDir := *web
	if webDir == "" && !*dev {
		webDir = brand.WebDir
	}
	if webDir != "" {
		if webDir, err = filepath.Abs(webDir); err != nil {
			log.Fatal(err)
		}
	}
	pluginDirs := []string{brand.PackagedPluginsDir, brand.InstalledPluginsDir}
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
		ConfigPath:    cfgAbs,
		Dev:           *dev,
		NoAuth:        *noAuth,
		Listen:        *listen,
		WebDir:        webDir,
		ViteURL:       *vite,
		Bridge:        bp,
		PluginDirs:    pluginDirs,
		DevPluginsDir: devPlugins,
		SessionHelper: helper,
		Logger:        log.Default(),
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	signal.Ignore(syscall.SIGPIPE)
	if err := srv.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
