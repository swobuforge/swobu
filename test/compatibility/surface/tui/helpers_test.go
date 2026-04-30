package tui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

const (
	defaultJourneyTimeout       = 30 * time.Second
	defaultSmallViewportTimeout = 10 * time.Second
	defaultEndpointReadyTimeout = 20 * time.Second
)

func mustTimeoutFromEnvOrDefault(t *testing.T, envName string, fallback time.Duration) time.Duration {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(envName))
	if value == "" {
		return fallback
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		t.Fatalf("invalid %s timeout %q (must be positive duration, e.g. 15s): %v", envName, value, err)
	}
	return timeout
}

func startJourney(t *testing.T, cols int, rows int) harness.OperatorPTYJourney {
	t.Helper()
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{})
	return startJourneyWithDaemon(t, daemon.BaseURL, cols, rows)
}

func startJourneyWithDaemon(t *testing.T, daemonURL string, cols int, rows int) harness.OperatorPTYJourney {
	t.Helper()
	t.Setenv("SWOBU_DAEMON_URL", daemonURL)
	readyTimeout := mustTimeoutFromEnvOrDefault(t, "SWOBU_CONTRACT_TUI_ENDPOINT_READY_TIMEOUT", defaultEndpointReadyTimeout)
	var firstRunFallback *harness.OperatorPTYJourney
	for attempt := 0; attempt < 2; attempt++ {
		journey := startJourneyBase(t, cols, rows)
		// UI readiness is the contract truth: either endpoint-backed workspace
		// rail, first-run prompt, or control-plane incompatibility screen must
		// appear before tests proceed.
		switch waitForWorkspaceReady(journey, readyTimeout) {
		case "rail", "incompatible":
			return journey
		case "first_run":
			// Prefer endpoint-backed rail when available; keep a first-run fallback
			// for tests that intentionally start with no configured endpoints.
			firstRunFallback = &journey
		}
	}
	if firstRunFallback != nil {
		return *firstRunFallback
	}
	t.Fatalf("journey did not reach workspace readiness after retry; daemon_url=%s", daemonURL)
	return harness.OperatorPTYJourney{}
}

func startJourneyWithDaemonAndWorkspaceRail(t *testing.T, daemonURL string, cols int, rows int, needles ...string) harness.OperatorPTYJourney {
	t.Helper()
	isolateJourneyHome(t)
	t.Setenv("SWOBU_DAEMON_URL", daemonURL)
	readyTimeout := mustTimeoutFromEnvOrDefault(t, "SWOBU_CONTRACT_TUI_ENDPOINT_READY_TIMEOUT", defaultEndpointReadyTimeout)
	for attempt := 0; attempt < 5; attempt++ {
		journey := startJourneyBase(t, cols, rows)
		if waitForWorkspaceRail(journey, readyTimeout, needles...) {
			return journey
		}
	}
	t.Fatalf("journey did not reach workspace rail readiness after retries; daemon_url=%s needles=%v", daemonURL, needles)
	return harness.OperatorPTYJourney{}
}

func waitForWorkspaceReady(journey harness.OperatorPTYJourney, timeout time.Duration) string {
	if timeout <= 0 {
		timeout = defaultEndpointReadyTimeout
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		visible := journey.VisibleOutput()
		if strings.Contains(visible, "[›") {
			return "rail"
		}
		if strings.Contains(visible, "daemon mismatch") {
			return "incompatible"
		}
		if strings.Contains(visible, "choose a workspace name") {
			return "first_run"
		}
		time.Sleep(50 * time.Millisecond)
	}
	return ""
}

func waitForWorkspaceRail(journey harness.OperatorPTYJourney, timeout time.Duration, needles ...string) bool {
	if timeout <= 0 {
		timeout = defaultEndpointReadyTimeout
	}
	if len(needles) == 0 {
		needles = []string{"[›"}
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		visible := journey.VisibleOutput()
		// Stable readiness signal: workspace rail is rendered.
		if strings.Contains(visible, "[›") {
			return true
		}
		for _, needle := range needles {
			if strings.Contains(visible, needle) {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func startJourneyBase(t *testing.T, cols int, rows int) harness.OperatorPTYJourney {
	t.Helper()
	timeout := mustTimeoutFromEnvOrDefault(t, "SWOBU_CONTRACT_TUI_JOURNEY_TIMEOUT", defaultJourneyTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	j := harness.StartSwobuOperatorPTYJourney(ctx, t, cols, rows)
	j.WaitVisible("Swobu")
	return j
}

func isolateJourneyHome(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("SWOBU_TEST_KEEP_HOME")) != "" {
		return
	}
	t.Setenv("HOME", t.TempDir())
}

func newChatCompletionsUpstream(t *testing.T, chatStatus int, chatBody string) *httptest.Server {
	t.Helper()

	if chatStatus <= 0 {
		chatStatus = http.StatusOK
	}
	if chatBody == "" {
		chatBody = `{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"gpt-4.1-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4.1-mini","object":"model"}]}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(chatStatus)
			_, _ = w.Write([]byte(chatBody))
		default:
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}
	}))
}

func startDaemonWithOneCustomEndpoint(t *testing.T, endpointName string) harness.DaemonProcessHarness {
	t.Helper()
	upstream := newChatCompletionsUpstream(t, http.StatusOK, "")
	t.Cleanup(upstream.Close)

	return harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(
				t,
				endpointName,
				"backend-a",
				harness.NewProviderConfig(
					t,
					"backend-a",
					"custom",
					upstream.URL+"/v1",
					"env",
					protocolsurface.ChatCompletions,
				),
			),
		},
	})
}
