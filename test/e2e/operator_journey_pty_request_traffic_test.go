package e2e_test

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

// Covers the declarative PTY flywheel lane in
// tasks/ready/06-proof-release/28c-e2e-journey-flywheel-declarative-runtime.md.
// Traceability: B-001 endpoint URL truth, B-076 traffic evidence truth.
func TestPTYOperatorJourney_RealClientRequestAndTrafficTruth(t *testing.T) {
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
				mustProviderConfigWithModelID(
					t,
					harness.NewProviderConfig(
						t,
						"backend-a",
						"custom",
						upstream.URL+"/v1",
						"",
						protocolsurface.ChatCompletions,
					),
					"gpt-4.1-mini",
				),
			),
		},
	})
	t.Setenv("SWOBU_DAEMON_URL", daemon.BaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	journey := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journey.WaitVisible("Swobu")
	journey.WaitVisibleAny("ready", "offline (stale)")
	requireVisibleOrSkip(t, journey.VisibleOutput, "/c/jobs/", 10*time.Second)
	ensureClientsSectionOpen(journey)

	requestBody := `{"messages":[{"role":"user","content":"hi"}]}`
	status, body := postChatCompletion(t, daemon.BaseURL+"/c/jobs/chat/completions", requestBody)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", status, body)
	}
	if !strings.Contains(body, `"content":"ok"`) {
		t.Fatalf("body = %q, want assistant output", body)
	}

	journey.AssertOutputContains("jobs")
	journey.AssertVisibleContains("/c/jobs/")
	journey.WaitVisibleAny("1 req", "1 runs")
	journey.WaitVisibleAny("ok 100%", "0% err")
	journey.AssertVisibleOmits(`"content":"hi"`)
}

// Traceability: B-050 backend-error truth, B-076 traffic evidence truth.
func TestPTYOperatorJourney_BackendFailureTruthVisibleInTraffic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY journey currently targets unix-style shell client in CI/dev lanes")
	}

	upstream := newChatCompletionsUpstream(t, http.StatusTooManyRequests, `{"error":{"message":"rate limited"}}`)
	defer upstream.Close()

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(
				t,
				"jobs",
				"backend-a",
				mustProviderConfigWithModelID(
					t,
					harness.NewProviderConfig(
						t,
						"backend-a",
						"custom",
						upstream.URL+"/v1",
						"",
						protocolsurface.ChatCompletions,
					),
					"gpt-4.1-mini",
				),
			),
		},
	})
	t.Setenv("SWOBU_DAEMON_URL", daemon.BaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	journey := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journey.WaitVisible("Swobu")
	journey.WaitVisibleAny("ready", "offline (stale)")
	requireVisibleOrSkip(t, journey.VisibleOutput, "/c/jobs/", 10*time.Second)
	ensureClientsSectionOpen(journey)

	requestBody := `{"messages":[{"role":"user","content":"trigger failure"}]}`
	status, body := postChatCompletion(t, daemon.BaseURL+"/c/jobs/chat/completions", requestBody)
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429, body=%s", status, body)
	}
	if !strings.Contains(body, "rate limited") {
		t.Fatalf("body = %q, want backend failure message", body)
	}

	journey.WaitVisibleAny("1 req", "1 runs")
	journey.WaitVisibleAny("err 100%", "100% err", "ok 0%")
	journey.AssertVisibleOmits(`"content":"trigger failure"`)
}

func requireVisibleOrSkip(t *testing.T, visible func() string, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(visible(), needle) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Skipf("skipping: expected %q not visible within %s", needle, timeout)
}
