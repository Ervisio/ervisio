package server

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/maintenance"
)

func TestResetPreviewCapabilityIsBoundToSessionAndExpiry(t *testing.T) {
	s := &Server{}
	state := s.resetState()
	token := strings.Repeat("a", 64)
	state.plans[token] = resetPlan{plan: &maintenance.Plan{ID: strings.Repeat("b", 32)}, owner: "original", expires: time.Now().Add(time.Minute)}
	request := httptest.NewRequest("POST", "https://console.example/api/rpc", nil)
	params := json.RawMessage(`{"token":"` + token + `","confirm":"RESET ERVISIO"}`)
	_, err := s.resetCall(request, &Session{key: "different"}, rpcRequest{Method: "maintenance.reset.apply", Params: params})
	if err == nil {
		t.Fatal("another session consumed the preview")
	}
	if _, ok := state.plans[token]; !ok {
		t.Fatal("failed authorization consumed preview")
	}
	item := state.plans[token]
	item.expires = time.Now().Add(-time.Minute)
	state.plans[token] = item
	if _, err = s.resetCall(request, &Session{key: "original"}, rpcRequest{Method: "maintenance.reset.apply", Params: params}); err == nil {
		t.Fatal("expired preview accepted")
	}
	if _, err = s.resetCall(request, &Session{key: "original"}, rpcRequest{Method: "maintenance.reset.apply", Params: json.RawMessage(`{"confirm":"yes"}`)}); err == nil {
		t.Fatal("missing explicit confirmation accepted")
	}
}

func TestResetOptionsRefuseUnmanagedLocations(t *testing.T) {
	for name, options := range map[string]Options{
		"config":       {ConfigPath: filepath.Join(t.TempDir(), "custom.conf")},
		"state":        {ConfigPath: brand.ConfigPath, StateDir: "/custom/state"},
		"notify":       {ConfigPath: brand.ConfigPath, NotifyFile: "/custom/notify.json"},
		"environments": {ConfigPath: brand.ConfigPath, EnvsDir: "/custom/envs"},
		"tunnels":      {ConfigPath: brand.ConfigPath, TunnelDir: "/custom/tunnels"},
		"listen":       {ConfigPath: brand.ConfigPath, Listen: "127.0.0.1:8080"},
	} {
		t.Run(name, func(t *testing.T) {
			s := &Server{opts: options}
			if _, err := s.resetOptions(httptest.NewRequest("POST", "https://console.example/api/rpc", nil), &Session{Account: &account.Account{Name: "operator"}}, true); err == nil {
				t.Fatal("unmanaged configuration accepted")
			}
		})
	}
}

func TestResetDoesNotAcceptAnonymousRequests(t *testing.T) {
	s := &Server{}
	if s.resetAdmin(nil) == nil || s.resetAdmin(&Session{}) == nil {
		t.Fatal("anonymous reset allowed")
	}
	response := httptest.NewRecorder()
	s.handleResetPublicStatus(response, httptest.NewRequest("GET", "/api/maintenance/reset/status?id=../../etc/passwd&token=short", nil))
	if response.Code != 404 {
		t.Fatalf("invalid status capability returned %d", response.Code)
	}
}
