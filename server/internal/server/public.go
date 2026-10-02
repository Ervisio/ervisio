package server

import (
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/sys"
)

type publicDistro struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	Logo    string `json:"logo,omitempty"`
	LogoURL string `json:"logoUrl,omitempty"`
}

type publicHost struct {
	Hostname string       `json:"hostname"`
	IP       string       `json:"ip,omitempty"`
	Distro   publicDistro `json:"distro"`
	// SSHKeys: the sign-in page may offer "Sign in with an SSH key" (auth.ssh_keys).
	SSHKeys bool `json:"sshKeys"`
}

// hostCache avoids re-reading os-release and probing the network on every
// sign-in page load.
var hostCache struct {
	sync.Mutex
	at   time.Time
	host publicHost
	ip   string
	logo string
}

func cachedHost() (publicHost, string, string) {
	hostCache.Lock()
	defer hostCache.Unlock()
	if time.Since(hostCache.at) > 30*time.Second {
		osr := sys.ReadOSRelease()
		logo := sys.FindLogo(osr.Logo)
		hostCache.host = publicHost{
			Hostname: sys.Hostname(),
			Distro:   publicDistro{ID: osr.ID, Name: osr.PrettyName, Color: sys.DistroColor(osr.ID), Logo: osr.Logo},
		}
		if logo != "" {
			hostCache.host.Distro.LogoURL = "/api/public/logo"
		}
		hostCache.ip = sys.PrimaryIP()
		hostCache.logo = logo
		hostCache.at = time.Now()
	}
	return hostCache.host, hostCache.ip, hostCache.logo
}

func (s *Server) handlePublicHost(w http.ResponseWriter, r *http.Request) {
	h, ip, _ := cachedHost()
	if s.Config().Login.ShowIP {
		h.IP = ip
	}
	h.SSHKeys = s.Config().Auth.SSHKeys
	writeJSON(w, http.StatusOK, h)
}

// handlePublicLogo serves the distribution logo named by os-release LOGO.
func (s *Server) handlePublicLogo(w http.ResponseWriter, r *http.Request) {
	_, _, logo := cachedHost()
	if logo == "" {
		writeError(w, errNotFound)
		return
	}
	f, err := os.Open(logo)
	if err != nil {
		writeError(w, errNotFound)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.Size() > 4<<20 {
		writeError(w, errNotFound)
		return
	}
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// startedAt is when this daemon process started (unix ms), reported by
// /api/health so a client can tell a restarted daemon from the old one.
var startedAt = time.Now().UnixMilli()

// handleHealth answers GET /api/health without authentication: the web
// app polls it while the daemon restarts after an update, and the update
// helper uses it to decide whether to roll back.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": brand.Version, "startedAt": startedAt})
}
