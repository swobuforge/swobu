package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestDaemonProcessTelemetryDebug_EmitsTelemetryToStdout(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telemetry-state.json")
	state := `{"enabled":true,"anonymous_install_id":"anon_e2e","first_seen_at":"2026-01-01T00:00:00Z","notice_shown":true}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Env: map[string]string{
			"SWOBU_TELEMETRY_DEBUG":      "1",
			"SWOBU_TELEMETRY_INTERVAL":   "1",
			"SWOBU_TELEMETRY_STATE_PATH": statePath,
		},
	})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		stdout := daemon.Stdout()
		if strings.Contains(stdout, `"telemetry_debug":true`) && strings.Contains(stdout, `"kind":"install"`) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("stdout missing telemetry install debug payload; stdout=%s stderr=%s", daemon.Stdout(), daemon.Stderr())
}
