package tui_test

import (
	"context"
	"testing"

	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestB072_CanvasFocusSemantics_TraversesVisibleRowsAndHonorsMinimumViewport(t *testing.T) {
	daemon := startDaemonWithOneCustomEndpoint(t, "acme")
	journey := startJourneyWithDaemon(t, daemon.BaseURL, 160, 50)
	journey.FocusRowDown("routing")
	journey.ActivateFocusedRow()
	journey.FocusRowDown("run on")

	timeout := mustTimeoutFromEnvOrDefault(t, "SWOBU_CONTRACT_TUI_SMALL_VIEWPORT_TIMEOUT", defaultSmallViewportTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	small := harness.StartSwobuOperatorPTYJourney(ctx, t, 40, 12)
	small.WaitVisible("Terminal too small")
}
