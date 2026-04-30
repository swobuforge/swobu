package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestPTYOperatorJourney_TelemetryDisclosureSuppressedWhenAlreadyShown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY journey currently targets unix-style shell client in CI/dev lanes")
	}

	root := t.TempDir()
	statePath := filepath.Join(root, "telemetry", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	preset := `{
  "enabled": true,
  "anonymous_install_id": "anon_existing",
  "first_seen_at": "2026-04-29T00:00:00Z",
  "notice_shown": true
}`
	if err := os.WriteFile(statePath, []byte(preset), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	t.Setenv("SWOBU_TELEMETRY_STATE_PATH", statePath)
	t.Setenv("SWOBU_DAEMON_URL", "http://127.0.0.1:1")
	t.Setenv("SWOBU_CONFIG_PATH", filepath.Join(root, "missing-config.yaml"))

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()

	run := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	run.WaitVisibleAny("daemon unavailable at", "attach-or-start failed", "unable to connect", "daemon readiness failed")
	visible := run.VisibleOutput()

	if strings.Contains(visible, "Swobu sends anonymous aggregate reliability and usage summaries by default (opt-out).") {
		t.Fatalf("disclosure was shown despite preset notice_shown=true; visible=%q", visible)
	}
	for _, forbidden := range []string{"api_key", "authorization", "prompt", "content"} {
		if strings.Contains(strings.ToLower(visible), forbidden) {
			t.Fatalf("visible output leaked forbidden token %q; visible=%q", forbidden, visible)
		}
	}
}
