package e2e_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/metrofun/swobu/test/e2e/harness"
)

// Covers launcher lifecycle truth: `swobu` attach-or-start waits for readiness
// and only then enters cockpit.
func TestPTYOperatorJourney_LauncherStartsDaemonAndEntersCockpitAfterReadiness(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY journey currently targets unix-style shell client in CI/dev lanes")
	}

	bindAddr := reserveLoopbackAddr(t)
	t.Setenv("SWOBU_DAEMON_URL", "http://"+bindAddr)
	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	t.Setenv("SWOBU_CONFIG_PATH", configPath)
	writeLauncherConfig(t, configPath, bindAddr)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	journey := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journey.WaitVisible("Swobu")
	journey.WaitVisibleAny("ready", "offline (stale)")
	journey.AssertVisibleOmits("unavailable at")
}

func reserveLoopbackAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return addr
}

func writeLauncherConfig(t *testing.T, path string, bindAddr string) {
	t.Helper()

	content := fmt.Sprintf("bind_addr: %s\nendpoints:\n  []\n", bindAddr)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
