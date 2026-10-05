//go:build !windows

package software

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// The scheduled update is a systemd timer pair on Linux; on Windows it is a
// Task Scheduler task (schedule_task_test.go).

func TestSchedule(t *testing.T) {
	old := systemdDir
	systemdDir = t.TempDir()
	defer func() { systemdDir = old }()
	if currentSchedule() != "" {
		t.Error("no timer yet")
	}
	os.WriteFile(filepath.Join(systemdDir, timerName), []byte(renderTimer("03:00")), 0o644)
	if got := currentSchedule(); got != "03:00" {
		t.Errorf("got %q", got)
	}
	m := &manager{primary: newPacman()}
	bad := "3am"
	if _, err := m.setSchedule(context.Background(), &bad); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("bad time: %v", err)
	}
	svc, err := renderService([]Plan{{Steps: []Step{{Name: "sh", Args: []string{"-c", "echo a b"}, Env: []string{"X=1"}}}}, {Steps: []Step{{Name: "sh", Args: []string{"-c", "true"}}}}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(svc, `ExecStart=/`) || !strings.Contains(svc, `"echo a b"`) || !strings.Contains(svc, "ExecStart=-/") || !strings.Contains(svc, "Environment=X=1") || !strings.Contains(svc, "Type=oneshot") {
		t.Errorf("service:\n%s", svc)
	}
}
