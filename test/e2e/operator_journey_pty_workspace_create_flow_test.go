package e2e_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/metrofun/swobu/test/e2e/harness"
)

// Covers 27d workflow truth: create lane configures provider and saves
// workspace in the same cockpit canvas.
func TestPTYOperatorJourney_CreateWorkspaceConfiguresProviderAndSaves(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY journey currently targets unix-style shell client in CI/dev lanes")
	}

	bindAddr := reserveLoopbackAddr(t)
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{BindAddr: bindAddr})
	defer daemon.Close()
	t.Setenv("SWOBU_DAEMON_URL", "http://"+bindAddr)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	j := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	j.WaitVisible("Swobu")
	j.WaitVisibleAny("ready", "offline (stale)")
	j.WaitVisible("ready")

	j.FocusRow("routing")
	j.ActivateFocusedRow()
	j.WaitVisible("run on")
	j.FocusRow("run on")
	j.ActivateFocusedRow()
	j.FocusRow("Ollama")
	j.ActivateFocusedRow()
	j.WaitVisibleAny("run on            Ollama", "run on Ollama")

	j.FocusRow("name")
	j.ActivateFocusedRow()
	j.FocusRow("name")
	for i := 0; i < len("new-workspace"); i++ {
		j.SendKey("backspace")
	}
	j.TypeText("jobs")
	j.SendKey("enter")

	j.WaitVisible("jobs")
	j.WaitVisible("run on             Ollama")
}
