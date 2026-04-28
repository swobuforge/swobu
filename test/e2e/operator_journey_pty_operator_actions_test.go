package e2e_test

import (
	"context"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

// Covers truthful run wiring disclosure for a supported CLI profile without
// requiring interactive external client workflows to finish in PTY lanes.
// Traceability: clients run-once action truth.
func TestPTYOperatorJourney_QuickLaunchRunsForegroundAndResumesCockpit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY journey currently targets unix-style shell client in CI/dev lanes")
	}

	upstream := newChatCompletionsUpstream(t, http.StatusOK, "")
	defer upstream.Close()

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(
				t,
				"jobs",
				"backend-a",
				harness.NewProviderConfig(
					t,
					"backend-a",
					"custom",
					upstream.URL+"/v1",
					"",
					protocolsurface.ChatCompletions,
				),
			),
		},
	})
	t.Setenv("SWOBU_DAEMON_URL", daemon.BaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	journey := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journey.WaitVisible("Swobu")
	journey.WaitVisibleAny("ready", "offline (stale)")
	journey.WaitVisible("/c/jobs/")
	ensureClientsSectionOpen(journey)
	selectClientEvidence(journey, "Aider")
	journey.FocusRow("run")
	journey.WaitVisible("run")
	journey.WaitVisible("command")
	journey.AssertVisibleOmits("not verified")
}

// Covers truthful copy behavior notes without depending on host clipboard
// availability. The cockpit must report one truthful post-copy outcome.
// Traceability: task 28c non-network operator action truth.
func TestPTYOperatorJourney_CopyBaseURLShowsTruthfulOutcomeNote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY journey currently targets unix-style shell client in CI/dev lanes")
	}

	upstream := newChatCompletionsUpstream(t, http.StatusOK, "")
	defer upstream.Close()

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(
				t,
				"jobs",
				"backend-a",
				harness.NewProviderConfig(
					t,
					"backend-a",
					"custom",
					upstream.URL+"/v1",
					"",
					protocolsurface.ChatCompletions,
				),
			),
		},
	})
	t.Setenv("SWOBU_DAEMON_URL", daemon.BaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	journey := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journey.WaitVisible("Swobu")
	journey.WaitVisibleAny("ready", "offline (stale)")
	journey.WaitVisible("/c/jobs/")
	ensureClientsSectionOpen(journey)

	journey.FocusClientAccessRow()
	journey.CopyClientBaseURLFromClientAccess()
	journey.WaitVisible("copy values")
	journey.WaitVisible("/c/jobs/")
}

func ensureClientsSectionOpen(journey harness.OperatorPTYJourney) {
	journey.EnsureClientsSectionOpen()
}
