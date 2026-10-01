// Package modules wires every bridge module into a registry.
package modules

import (
	configmod "github.com/Fonlogen/LinuxAdmin/server/internal/modules/config"
	"github.com/Fonlogen/LinuxAdmin/server/internal/modules/files"
	"github.com/Fonlogen/LinuxAdmin/server/internal/modules/logs"
	"github.com/Fonlogen/LinuxAdmin/server/internal/modules/plugins"
	"github.com/Fonlogen/LinuxAdmin/server/internal/modules/prefs"
	"github.com/Fonlogen/LinuxAdmin/server/internal/modules/services"
	"github.com/Fonlogen/LinuxAdmin/server/internal/modules/software"
	"github.com/Fonlogen/LinuxAdmin/server/internal/modules/system"
	"github.com/Fonlogen/LinuxAdmin/server/internal/modules/terminal"
	"github.com/Fonlogen/LinuxAdmin/server/internal/modules/users"
	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// RegisterAll registers every module. Each section owns one line.
func RegisterAll(r *rpc.Registry) {
	system.Register(r)
	prefs.Register(r)
	configmod.Register(r)
	terminal.Register(r)
	files.Register(r)
	logs.Register(r)
	services.Register(r)
	software.Register(r)
	users.Register(r)
	plugins.Register(r)
}
