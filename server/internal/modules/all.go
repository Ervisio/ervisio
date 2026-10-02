// Package modules wires every bridge module into a registry.
package modules

import (
	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
	"github.com/ervisio/ervisio/server/internal/modules/files"
	"github.com/ervisio/ervisio/server/internal/modules/logs"
	"github.com/ervisio/ervisio/server/internal/modules/overview"
	"github.com/ervisio/ervisio/server/internal/modules/plugins"
	"github.com/ervisio/ervisio/server/internal/modules/prefs"
	"github.com/ervisio/ervisio/server/internal/modules/services"
	"github.com/ervisio/ervisio/server/internal/modules/software"
	"github.com/ervisio/ervisio/server/internal/modules/system"
	"github.com/ervisio/ervisio/server/internal/modules/terminal"
	"github.com/ervisio/ervisio/server/internal/modules/updates"
	"github.com/ervisio/ervisio/server/internal/modules/users"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// RegisterAll registers every module. Each section owns one line.
func RegisterAll(r *rpc.Registry) {
	system.Register(r)
	overview.Register(r)
	prefs.Register(r)
	configmod.Register(r)
	terminal.Register(r)
	files.Register(r)
	logs.Register(r)
	services.Register(r)
	software.Register(r)
	users.Register(r)
	plugins.Register(r)
	updates.Register(r)
}
