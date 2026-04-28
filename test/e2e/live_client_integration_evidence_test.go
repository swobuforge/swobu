package e2e_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestLiveClientIntegrationEvidence_OpenRouter(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_EVIDENCE")) != "1" {
		t.Skip("set SWOBU_LIVE_EVIDENCE=1 to run live client integration evidence")
	}
	if runtime.GOOS == "windows" {
		t.Skip("PTY journey currently targets unix-style shells")
	}
	requireLiveOpenRouterToken(t)
	if _, err := exec.LookPath("aider"); err != nil {
		t.Fatalf("aider binary is required for live client evidence: %v", err)
	}

	provider := harness.NewProviderConfig(
		t,
		"or-main",
		"openrouter",
		"https://openrouter.ai/api/v1",
		"env:OPENROUTER_API_KEY",
		protocolsurface.ChatCompletions,
	)
	provider, err := provider.WithModelID("openai/gpt-4o-mini")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "jobs", "or-main", provider),
		},
	})
	t.Setenv("SWOBU_DAEMON_URL", daemon.BaseURL)
	assertLiveSwobuOpenRouterResponse(t, daemon.BaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	journey := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journey.WaitVisible("Swobu")
	journey.WaitVisibleAny("ready", "offline (stale)")
	waitForEndpointPathEvidence(journey)
	openClientsSectionEvidence(journey)

	selectClientEvidence(journey, "Codex")
	journey.FocusRow("file config")
	journey.ActivateFocusedRow()
	journey.WaitVisible("config.toml")
	journey.WaitVisible("openai_base_url")

	journeyClaude := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journeyClaude.WaitVisible("Swobu")
	journeyClaude.WaitVisibleAny("ready", "offline (stale)")
	waitForEndpointPathEvidence(journeyClaude)
	openClientsSectionEvidence(journeyClaude)

	selectClientEvidence(journeyClaude, "Claude")
	journeyClaude.FocusRow("environment values")
	journeyClaude.ActivateFocusedRow()
	journeyClaude.WaitVisible("ANTHROPIC_BASE_URL")
	journeyClaude.WaitVisible("ANTHROPIC_MODEL")

	journeyAider := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journeyAider.WaitVisible("Swobu")
	journeyAider.WaitVisibleAny("ready", "offline (stale)")
	waitForEndpointPathEvidence(journeyAider)
	openClientsSectionEvidence(journeyAider)

	selectClientEvidence(journeyAider, "Aider")
	journeyAider.FocusRow("file config")
	journeyAider.ActivateFocusedRow()
	journeyAider.WaitVisible(".aider.conf.yml")
	journeyAider.WaitVisible("openai-api-base")
	journeyAider.WaitVisible("model: openai/primary")
	journeyAider.FocusRow("run")
	journeyAider.ActivateFocusedRow()
	waitForRunExitOrInterruptEvidence(journeyAider, "aider", 25*time.Second)
	waitForCockpitReturnEvidence(journeyAider, 25*time.Second)

	journeyOpenCode := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journeyOpenCode.WaitVisible("Swobu")
	journeyOpenCode.WaitVisibleAny("ready", "offline (stale)")
	waitForEndpointPathEvidence(journeyOpenCode)
	openClientsSectionEvidence(journeyOpenCode)

	selectClientEvidence(journeyOpenCode, "OpenCode")
	journeyOpenCode.FocusRow("file config")
	journeyOpenCode.ActivateFocusedRow()
	journeyOpenCode.WaitVisible("opencode.json")
	journeyOpenCode.WaitVisible("opencode.ai/config.json")
	journeyOpenCode.WaitVisible(`"model": "swobu/primary"`)

	if _, err := exec.LookPath("cn"); err == nil {
		journeyContinue := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
		journeyContinue.WaitVisible("Swobu")
		journeyContinue.WaitVisibleAny("ready", "offline (stale)")
		waitForEndpointPathEvidence(journeyContinue)
		openClientsSectionEvidence(journeyContinue)

		selectClientEvidence(journeyContinue, "Continue")
		journeyContinue.FocusRow("file config")
		journeyContinue.ActivateFocusedRow()
		journeyContinue.WaitVisible("swobu.continue.yaml")
		journeyContinue.WaitVisible("apiBase:")
	} else {
		t.Log("continue client binary (cn) not found; skipping continue manual command presence check")
	}
}

func TestLiveClientIntegrationEvidence_OpenRouter_AiderOnly(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_EVIDENCE")) != "1" {
		t.Skip("set SWOBU_LIVE_EVIDENCE=1 to run live client integration evidence")
	}
	if runtime.GOOS == "windows" {
		t.Skip("PTY journey currently targets unix-style shells")
	}
	requireLiveOpenRouterToken(t)
	if _, err := exec.LookPath("aider"); err != nil {
		t.Fatalf("aider binary is required for live client evidence: %v", err)
	}

	provider := harness.NewProviderConfig(
		t,
		"or-main",
		"openrouter",
		"https://openrouter.ai/api/v1",
		"env:OPENROUTER_API_KEY",
		protocolsurface.ChatCompletions,
	)
	provider, err := provider.WithModelID("openai/gpt-4o-mini")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "jobs", "or-main", provider),
		},
	})
	t.Setenv("SWOBU_DAEMON_URL", daemon.BaseURL)
	assertLiveSwobuOpenRouterResponse(t, daemon.BaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	journeyAider := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journeyAider.WaitVisible("Swobu")
	journeyAider.WaitVisibleAny("ready", "offline (stale)")
	waitForEndpointPathEvidence(journeyAider)
	openClientsSectionEvidence(journeyAider)

	selectClientEvidence(journeyAider, "Aider")
	journeyAider.FocusRow("file config")
	journeyAider.ActivateFocusedRow()
	journeyAider.WaitVisible(".aider.conf.yml")
	journeyAider.WaitVisible("openai-api-base")
	journeyAider.WaitVisible("model: openai/primary")
	journeyAider.FocusRow("run")
	journeyAider.ActivateFocusedRow()
	waitForRunExitOrInterruptEvidence(journeyAider, "aider", 25*time.Second)
	waitForCockpitReturnEvidence(journeyAider, 25*time.Second)

}

func assertLiveSwobuOpenRouterResponse(t *testing.T, baseURL string) {
	t.Helper()
	body := []byte(`{"model":"primary","messages":[{"role":"user","content":"Say exactly: pong"}],"max_tokens":16}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, strings.TrimRight(baseURL, "/")+"/c/jobs/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build swobu request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("swobu request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		t.Fatalf("swobu request status=%d body=%s", resp.StatusCode, string(raw))
	}
	if !strings.Contains(string(raw), `"choices"`) && !strings.Contains(string(raw), `"id"`) {
		t.Fatalf("unexpected success payload: %s", string(raw))
	}
}

func openClientsSectionEvidence(journey harness.OperatorPTYJourney) {
	journey.EnsureClientsSectionOpen()
}

func selectClientEvidence(journey harness.OperatorPTYJourney, label string) {
	journey.SelectClient(label)
}

func waitForRunExitOrInterruptEvidence(journey harness.OperatorPTYJourney, binary string, wait time.Duration) {
	deadline := time.Now().Add(wait)
	needleCompact := strings.ToLower(binary) + "exitedwithcode"
	for time.Now().Before(deadline) {
		visibleCompact := strings.ToLower(strings.Join(strings.Fields(journey.VisibleOutput()), ""))
		if strings.Contains(visibleCompact, needleCompact) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	postDeadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(postDeadline) {
		journey.TypeText("\x03")
		visibleCompact := strings.ToLower(strings.Join(strings.Fields(journey.VisibleOutput()), ""))
		if strings.Contains(visibleCompact, needleCompact) || looksLikeCockpitEvidence(journey.VisibleOutput()) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func requireClientBinaryEvidence(t *testing.T, binary string) {
	t.Helper()
	if _, err := exec.LookPath(binary); err != nil {
		t.Fatalf("%s binary is required for this test: %v", binary, err)
	}
}

func waitForCockpitReturnEvidence(journey harness.OperatorPTYJourney, wait time.Duration) {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		visible := journey.VisibleOutput()
		if looksLikeCockpitEvidence(visible) {
			return
		}
		journey.TypeText("\x03")
		journey.SendKey("esc")
		time.Sleep(300 * time.Millisecond)
	}
}

func looksLikeCockpitEvidence(visible string) bool {
	lower := strings.ToLower(visible)
	hasEndpointPath := strings.Contains(lower, "/c/jobs/") || strings.Contains(lower, "/c/<slug>/")
	return strings.Contains(lower, "swobu") &&
		strings.Contains(lower, "workspace") &&
		hasEndpointPath
}

func waitForEndpointPathEvidence(journey harness.OperatorPTYJourney) {
	journey.WaitVisibleAny("/c/jobs/", "/c/<slug>/")
}

func requireLiveOpenRouterToken(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) != "" {
		return
	}
	addCandidate := func(values []string, path string) []string {
		path = strings.TrimSpace(path)
		if path == "" {
			return values
		}
		for _, value := range values {
			if value == path {
				return values
			}
		}
		return append(values, path)
	}
	candidates := make([]string, 0, 8)
	candidates = addCandidate(candidates, strings.TrimSpace(os.Getenv("SWOBU_OPENROUTER_KEY_FILE")))
	candidates = addCandidate(candidates, ".secrets/openrouter.key")
	candidates = addCandidate(candidates, "openrouter.key")
	for _, root := range []string{"..", "../..", "../../.."} {
		candidates = addCandidate(candidates, filepath.Join(root, ".secrets", "openrouter.key"))
		candidates = addCandidate(candidates, filepath.Join(root, "openrouter.key"))
	}
	for _, path := range candidates {
		if strings.TrimSpace(path) == "" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		token := strings.TrimSpace(string(raw))
		if token == "" {
			continue
		}
		t.Setenv("OPENROUTER_API_KEY", token)
		t.Setenv("SWOBU_OPENROUTER_KEY_FILE", path)
		return
	}
	t.Fatal("missing OpenRouter credentials: set OPENROUTER_API_KEY or provide SWOBU_OPENROUTER_KEY_FILE (default .secrets/openrouter.key)")
}
