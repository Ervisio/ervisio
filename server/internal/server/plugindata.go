package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ervisio/ervisio/server/internal/plugindata"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

var privatePluginID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)

func (s *Server) handlePluginData(w http.ResponseWriter, r *http.Request, sess *Session, req rpcRequest) bool {
	if !strings.HasPrefix(req.Method, "plugins.data.") {
		return false
	}
	operation := strings.TrimPrefix(req.Method, "plugins.data.")
	if operation != "read" && operation != "write" && operation != "list" && operation != "remove" && operation != "claim" && operation != "release" {
		writeError(w, rpc.Errorf(rpc.NotFound, "Unknown private data method"))
		return true
	}
	var p struct {
		Plugin   string `json:"plugin"`
		Key      string `json:"key"`
		Text     string `json:"text"`
		Revision string `json:"revision"`
		Owner    string `json:"owner"`
	}
	if json.Unmarshal(req.Params, &p) != nil || !privatePluginID.MatchString(p.Plugin) {
		writeError(w, rpc.Errorf(rpc.Invalid, "Invalid private plugin data request"))
		return true
	}
	// Every operation rechecks the enabled, signed, account-visible plugin.
	bridge, e := s.userBridge(r.Context(), sess)
	if e != nil {
		writeError(w, e)
		return true
	}
	access := map[string]string{"id": p.Plugin}
	if _, err := bridge.Call(r.Context(), "plugins.access", access); err != nil {
		writeError(w, rpc.ToError(err, true))
		return true
	}
	root, err := s.accountStateDirectory(sess)
	if err != nil {
		writeError(w, rpc.ToError(err, true))
		return true
	}
	for _, path := range []string{filepath.Join(root, "plugins"), filepath.Join(root, "plugins", p.Plugin), filepath.Join(root, "plugins", p.Plugin, "data")} {
		if err = privateStateDirectory(path); err != nil {
			writeError(w, rpc.ToError(err, true))
			return true
		}
	}
	directory := filepath.Join(root, "plugins", p.Plugin, "data")
	value, _ := s.pluginData.LoadOrStore(directory, plugindata.New(directory))
	store := value.(*plugindata.Store)
	var result any
	switch operation {
	case "read":
		result, err = store.Read(p.Key)
	case "write":
		result, err = store.Write(p.Key, p.Text, p.Revision, p.Owner)
	case "list":
		var entries []plugindata.Entry
		entries, err = store.List()
		result = map[string]any{"entries": entries}
	case "remove":
		err = store.Remove(p.Key, p.Revision)
		result = map[string]bool{"ok": err == nil}
	case "claim":
		result, err = store.Claim(p.Key, p.Owner)
	case "release":
		err = store.Release(p.Key, p.Owner)
		result = map[string]bool{"ok": err == nil}
	}
	if err != nil {
		writeError(w, rpc.ToError(err, true))
		return true
	}
	writeJSON(w, http.StatusOK, struct {
		Result any `json:"result"`
	}{result})
	return true
}
