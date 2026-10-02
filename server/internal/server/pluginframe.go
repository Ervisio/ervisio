package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// Plugin isolation (security review H2 and L2).
//
// Plugin code never runs in the app's origin. The web app embeds
// /plugin-frame/<id> in an <iframe sandbox="allow-scripts allow-forms"> (no
// allow-same-origin), so the frame has an opaque origin: no cookies, no
// access to the app's DOM or storage, and no way to call /api/*. The app
// fetches the plugin's entry module itself and hands it to the frame over
// postMessage, together with the theme; a broker in web/src/plugins answers
// the frame's requests within the plugin's manifest.

// pluginAssetCSP is sent with every /plugins/<id>/<file>: should such a file
// ever be opened as a document it is sandboxed and can run nothing.
const pluginAssetCSP = "sandbox; default-src 'none'; frame-ancestors 'none'"

// pluginRuntimePath is the runtime bundle (React, the UI kit and the SDK)
// the frame loads; built from web/src/plugins/frame into web/public.
const pluginRuntimePath = "/plugin-runtime.js"

// pluginHostRe accepts what plugins.access returns for capabilities.network
// (it validates the same way; checked again here because it goes into a header).
var pluginHostRe = regexp.MustCompile(`^(http://)?(\*\.)?[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*(:[0-9]{1,5})?$`)

type pluginAccessInfo struct {
	ID      string   `json:"id"`
	Network []string `json:"network"`
}

// pluginAccess asks the session's user bridge whether the plugin may be used
// by this user right now: it exists, is enabled, passes the signature policy
// and its visibleTo admits the user. Anything else (including an older bridge
// without plugins.access) is a refusal.
func (s *Server) pluginAccess(ctx context.Context, sess *Session, id string) (*pluginAccessInfo, bool) {
	const method = "plugins.access"
	b, e := s.userBridge(ctx, sess)
	if e != nil {
		return nil, false
	}
	if _, ok := b.Level(method); !ok {
		return nil, false
	}
	raw, err := b.Call(ctx, method, map[string]string{"id": id})
	if err != nil {
		return nil, false
	}
	var info pluginAccessInfo
	if json.Unmarshal(raw, &info) != nil || info.ID != id {
		return nil, false
	}
	return &info, true
}

// setPluginAssetHeaders marks a plugin file as inert data for the app origin.
func setPluginAssetHeaders(h http.Header) {
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", pluginAssetCSP)
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
}

// pluginFrameCSP is the policy of the plugin frame document. The frame runs
// sandboxed even when opened directly (the sandbox directive); scripts are
// only the nonce'd runtime and the blob: module the runtime creates from the
// code the app hands over; no network except the declared hosts. allow-forms
// only lets submit events fire (the UI kit's dialogs use them): form-action
// 'none' still stops any form from being sent anywhere.
func pluginFrameCSP(nonce string, hosts []string) string {
	connect := "'none'"
	if len(hosts) > 0 {
		var srcs []string
		for _, h := range hosts {
			switch {
			case !pluginHostRe.MatchString(h):
			case strings.HasPrefix(h, "http://"): // approved as plain http
				// Only http: an http approval is not one for unencrypted
				// WebSockets (ws://), which nobody approved.
				srcs = append(srcs, h)
			default:
				srcs = append(srcs, "https://"+h, "wss://"+h)
			}
		}
		if len(srcs) > 0 {
			connect = strings.Join(srcs, " ")
		}
	}
	return "default-src 'none'; script-src 'nonce-" + nonce + "' blob:; style-src 'unsafe-inline'; " +
		"img-src data: blob:; font-src data:; media-src data: blob:; connect-src " + connect + "; " +
		"form-action 'none'; base-uri 'none'; frame-ancestors 'self'; sandbox allow-scripts allow-forms"
}

// handlePluginFrame serves GET /plugin-frame/<id>: the empty host page of a
// plugin's sandboxed frame.
func (s *Server) handlePluginFrame(w http.ResponseWriter, r *http.Request, sess *Session) {
	id := r.PathValue("id")
	if !pluginIDRe.MatchString(id) {
		writeError(w, errNotFound)
		return
	}
	info, ok := s.pluginAccess(r.Context(), sess, id)
	if !ok {
		writeError(w, errNotFound)
		return
	}
	var rnd [18]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		http.Error(w, "no randomness", http.StatusInternalServerError)
		return
	}
	nonce := base64.RawURLEncoding.EncodeToString(rnd[:])
	h := w.Header()
	h.Del("X-Frame-Options") // framed by the app itself; frame-ancestors 'self' below
	h.Set("Content-Security-Policy", pluginFrameCSP(nonce, info.Network))
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<script nonce="` + nonce + `" src="` + pluginRuntimePath + `"></script>` +
		`</head><body><div id="root"></div></body></html>`))
}
