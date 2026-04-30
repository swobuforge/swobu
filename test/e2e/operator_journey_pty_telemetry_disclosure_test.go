package e2e_test

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestPTYOperatorJourney_TelemetryDisclosureIsShownBeforeLaunchAndOneShot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY journey currently targets unix-style shell client in CI/dev lanes")
	}

	root := t.TempDir()
	statePath := filepath.Join(root, "telemetry", "state.json")
	t.Setenv("SWOBU_TELEMETRY_STATE_PATH", statePath)
	t.Setenv("SWOBU_DAEMON_URL", "http://127.0.0.1:1")
	t.Setenv("SWOBU_CONFIG_PATH", filepath.Join(root, "missing-config.yaml"))

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()

	first := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	first.WaitVisible("Swobu sends anonymous aggregate reliability and usage summaries by")
	first.WaitVisibleAny("daemon unavailable at", "attach-or-start failed", "unable to connect", "daemon readiness failed")

	second := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	second.WaitVisibleAny("daemon unavailable at", "attach-or-start failed", "unable to connect", "daemon readiness failed")
	time.Sleep(200 * time.Millisecond)
	if strings.Contains(second.VisibleOutput(), "Swobu sends anonymous aggregate reliability and usage summaries by default (opt-out).") {
		t.Fatalf("disclosure notice repeated on second interactive launch; visible=%q", second.VisibleOutput())
	}
}
