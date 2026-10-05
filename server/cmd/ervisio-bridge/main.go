// Command ervisio-bridge does the system work for one signed-in user. It
// speaks newline-delimited JSON on stdin/stdout (see internal/rpc) and is
// started by ervisiod: once per session as the user, and once as root
// through sudo when the user unlocks administrator rights (--admin).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/legacy"
	"github.com/ervisio/ervisio/server/internal/modules"
	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
	"github.com/ervisio/ervisio/server/internal/modules/plugins"
	"github.com/ervisio/ervisio/server/internal/modules/updates"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
	"github.com/ervisio/ervisio/server/internal/update"
)

func main() {
	admin := flag.Bool("admin", false, "run as the root bridge (must be started as root)")
	configPath := flag.String("config", brand.ConfigPath, "daemon configuration file")
	dev := flag.Bool("dev", false, "the daemon runs in --dev (passed by ervisiod)")
	devPlugins := flag.String("dev-plugins", "", "with --dev: the daemon's ./plugins folder")
	devPluginsFile := flag.String("dev-plugins-file", "", "with --dev: where plugins.loadDev keeps its list (instead of the one in the home folder)")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *version {
		fmt.Println(brand.Version)
		return
	}

	log.SetFlags(0)
	log.SetPrefix(brand.BridgeBinary + ": ")

	if *admin && !bridge.Privileged() {
		log.Fatal("--admin requires root")
	}
	if *admin {
		rootEnv()
	}
	if !filepath.IsAbs(*configPath) {
		log.Fatal("--config must be an absolute path")
	}
	configmod.Path = filepath.Clean(*configPath)
	// ~/.config/linuxadmin of this account becomes ~/.config/ervisio (once).
	if u, err := user.LookupId(strconv.Itoa(os.Geteuid())); err == nil {
		if moved, err := legacy.MigrateUserDir(u.HomeDir); err != nil {
			log.Printf("copy %s to %s: %v", brand.LegacyUserDataDir, brand.UserDataDir, err)
		} else if moved {
			log.Printf("copied ~/%s to ~/%s", brand.LegacyUserDataDir, brand.UserDataDir)
		}
	}
	// A versioned install lists the plugins shipped with its own version.
	if vdir, ok := update.RunningVersionDir(); ok {
		plugins.SystemDir = filepath.Join(vdir, "plugins")
	}
	if *dev {
		plugins.DaemonDev = true
		updates.DaemonDev = true
		if *devPluginsFile != "" {
			if !filepath.IsAbs(*devPluginsFile) {
				log.Fatal("--dev-plugins-file must be an absolute path")
			}
			plugins.DevFile = filepath.Clean(*devPluginsFile)
		}
		if *devPlugins != "" {
			if !filepath.IsAbs(*devPlugins) {
				log.Fatal("--dev-plugins must be an absolute path")
			}
			plugins.DaemonPluginsDir = filepath.Clean(*devPlugins)
		}
	}

	// Commands are resolved in a fixed PATH; the environment we were given
	// (possibly by sudo) is not trusted for that.
	os.Setenv("PATH", sys.SafePath)

	proto, err := protocolStdout()
	if err != nil {
		log.Fatal(err)
	}

	reg := rpc.NewRegistry()
	modules.RegisterAll(reg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	signal.Ignore(syscall.SIGPIPE, syscall.SIGHUP)

	err = rpc.Serve(ctx, reg, os.Stdin, proto, rpc.ServeOptions{
		Admin:   *admin,
		UID:     os.Geteuid(),
		Version: brand.Version,
	})
	if err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}

// rootEnv makes the root bridge independent of where and how sudo started
// it: the working directory is "/" (not the user's home, which the user
// controls) and HOME/USER/LOGNAME are root's own, whatever sudoers keeps.
func rootEnv() {
	if err := os.Chdir("/"); err != nil {
		log.Fatalf("chdir /: %v", err)
	}
	home, name := "/root", "root"
	if u, err := user.LookupId(strconv.Itoa(os.Geteuid())); err == nil {
		if u.HomeDir != "" {
			home = u.HomeDir
		}
		name = u.Username
	}
	os.Setenv("HOME", home)
	os.Setenv("USER", name)
	os.Setenv("LOGNAME", name)
	os.Unsetenv("XDG_RUNTIME_DIR") // the user's runtime dir, if sudo kept it
}
