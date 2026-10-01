package services

import (
	"path"
	"regexp"
	"strings"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// unitNameRe matches systemd unit names: characters allowed by systemd.unit(5)
// (including "\" for escaped paths and "@" for templates/instances) followed
// by a known unit type suffix. It cannot contain "/" or whitespace.
var unitNameRe = regexp.MustCompile(`^[A-Za-z0-9:_.@\\-]+\.(service|socket|timer|target|mount|automount|path|slice|scope|swap|device)$`)

// ValidUnitName reports whether name is an acceptable systemd unit name.
func ValidUnitName(name string) bool {
	if len(name) > 255 || len(name) < 3 {
		return false
	}
	if name[0] == '-' || name[0] == '.' {
		return false
	}
	return unitNameRe.MatchString(name)
}

func checkName(name string) error {
	if !ValidUnitName(name) {
		return rpc.Errorf(rpc.Invalid, "%q is not a valid unit name. Use a name like nginx.service.", name)
	}
	return nil
}

// unitType returns the suffix of a unit name ("service", "timer", ...).
func unitType(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return ""
}

// ---- purpose heuristic (one table) ----

// Purpose groups, in the order the UI shows them.
const (
	PurposeWeb        = "web"
	PurposeContainers = "containers"
	PurposeSystem     = "system"
)

// purposeTable maps glob patterns (path.Match, on the lower-cased unit name
// without suffix and without the "@instance" part) to a purpose. First match wins.
var purposeTable = []struct {
	purpose  string
	patterns []string
}{
	{PurposeContainers, []string{
		"docker*", "containerd*", "podman*", "libvirt*", "virtqemud*", "virtlxcd*", "virtlogd*", "virtlockd*",
		"virtnetworkd*", "virtstoraged*", "virtnodedevd*", "virtsecretd*", "virtnwfilterd*", "virtinterfaced*",
		"virtproxyd*", "lxc*", "lxd*", "incus*", "crio*", "cri-o*", "k3s*", "kubelet*", "microk8s*", "snapd-lxd*",
	}},
	{PurposeWeb, []string{
		"nginx*", "apache*", "httpd*", "caddy*", "lighttpd*", "haproxy*", "traefik*", "varnish*", "php*-fpm*",
		"php-fpm*", "uwsgi*", "gunicorn*", "tomcat*", "postgresql*", "postgres*", "mysql*", "mariadb*",
		"redis*", "valkey*", "memcached*", "mongod*", "mongodb*", "elasticsearch*", "opensearch*", "grafana*",
		"prometheus*", "linuxadmin*", "node-red*", "php*",
	}},
}

// PurposeOf returns the purpose group of a unit.
func PurposeOf(name string) string {
	base := strings.ToLower(strings.TrimSuffix(name, "."+unitType(name)))
	if i := strings.IndexByte(base, '@'); i >= 0 {
		base = base[:i]
	}
	for _, g := range purposeTable {
		for _, p := range g.patterns {
			if ok, _ := path.Match(p, base); ok {
				return g.purpose
			}
		}
	}
	return PurposeSystem
}
