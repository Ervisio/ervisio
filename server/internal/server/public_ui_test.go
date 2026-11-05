package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/config"
)

func TestPublicHostDefaultShell(t *testing.T) {
	c := config.Default()
	c.UI.DefaultShell = "ervisios"
	s := &Server{cfg: &configHolder{cfg: c, checked: time.Now()}}
	w := httptest.NewRecorder()
	s.handlePublicHost(w, httptest.NewRequest("GET", "/api/public/host", nil))
	var h publicHost
	if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil || h.DefaultShell != "ervisios" {
		t.Fatalf("install default not discoverable: %s %v", w.Body.String(), err)
	}
}
