package tui_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

type viewportSize struct {
	cols int
	rows int
}

type cockpitScenario struct {
	name     string
	artifact string
	run      func(t *testing.T, viewport viewportSize) string
}

func TestCockpitScenarios_ExactScreenMatchesWireframeArtifacts(t *testing.T) {
	scenarios := []cockpitScenario{
		{name: "FirstRunInitialLaunch", artifact: "F-01_first-launch.txt", run: scenarioFirstRunInitialLaunch},
		{name: "FirstRunNameCommitted", artifact: "F-02_name-entered.txt", run: scenarioFirstRunNameCommitted},
		{name: "FirstRunProviderPickerOpen", artifact: "F-03_provider-picker-open.txt", run: scenarioFirstRunProviderPickerOpen},
		{name: "FirstRunProviderChosen", artifact: "F-04_provider-chosen.txt", run: scenarioFirstRunProviderChosen},
		{name: "FirstRunModelPickerOpen", artifact: "F-05_model-picker-open.txt", run: scenarioFirstRunModelPickerOpen},
		{name: "FirstRunReadyToCreate", artifact: "F-06_ready-to-create.txt", run: scenarioFirstRunReadyToCreate},
		{name: "FirstRunCreatingBusy", artifact: "F-07_creating.txt", run: scenarioFirstRunCreatingBusy},
		{name: "FirstRunSavedTransition", artifact: "F-08_success-transition.txt", run: scenarioFirstRunSavedTransition},
		{name: "Responsive80x24Compact", artifact: "R-01_80x24_compact_baseline.txt", run: scenarioResponsive80x24Compact},
		{name: "Responsive80x43Tall", artifact: "R-02_80x43_tall_reading_mode.txt", run: scenarioResponsive80x43Tall},
		{name: "Responsive132x24Wide", artifact: "R-03_132x24_wide_compact.txt", run: scenarioResponsive132x24WideCompact},
		{name: "Responsive132x43WideTall", artifact: "R-04_132x43_wide_tall.txt", run: scenarioResponsive132x43WideTall},
		{name: "ClientsChooseClient", artifact: "C-01_choose_client.txt", run: scenarioClientsChooseClient},
		{name: "ClientsCodexRunOnce", artifact: "C-02_codex_cli_run_once.txt", run: scenarioClientsCodexRunOnce},
		{name: "ClientsCodexCopyConfig", artifact: "C-03_codex_cli_copy_config.txt", run: scenarioClientsCodexCopyConfig},
		{name: "ClientsContinueCopySnippet", artifact: "C-04_continue_copy_snippet.txt", run: scenarioClientsContinueCopySnippet},
		{name: "ClientsOtherOpenAI", artifact: "C-05_other_openai_surface.txt", run: scenarioClientsOtherOpenAISurface},
		{name: "ClientsOtherAnthropic", artifact: "C-06_other_anthropic_surface.txt", run: scenarioClientsOtherAnthropicSurface},
		{name: "ClientsClaudeCopySnippet", artifact: "C-07_claude_code_copy_snippet.txt", run: scenarioClientsClaudeCodeCopySnippet},
		{name: "RoutingCredentialsProviderChosen", artifact: "CR-01_provider_chosen_env_key.txt", run: scenarioCRProviderChosenEnvKey},
		{name: "RoutingCredentialsEnvMissing", artifact: "CR-02_env_key_missing.txt", run: scenarioCREnvKeyMissing},
		{name: "RoutingCredentialsProviderPickerTop", artifact: "CR-03_provider_picker_top.txt", run: scenarioCRProviderPickerTop},
		{name: "RoutingCredentialsProviderPickerScrolled", artifact: "CR-04_provider_picker_scrolled.txt", run: scenarioCRProviderPickerScrolled},
		{name: "RoutingCredentialsKeychainNames", artifact: "CR-05_keychain_key_names.txt", run: scenarioCRKeychainKeyNames},
		{name: "RoutingCredentialsFileBrowserTop", artifact: "CR-06_file_browser_top.txt", run: scenarioCRFileBrowserTop},
		{name: "RoutingCredentialsFileBrowserScrolled", artifact: "CR-07_file_browser_scrolled.txt", run: scenarioCRFileBrowserScrolled},
		{name: "RoutingCredentialsFileChosen", artifact: "CR-08_file_chosen.txt", run: scenarioCRFileChosen},
		{name: "RoutingCredentialsModelPickerTop", artifact: "CR-09_model_picker_top.txt", run: scenarioCRModelPickerTop},
		{name: "RoutingCredentialsModelPickerScrolled", artifact: "CR-10_model_picker_scrolled.txt", run: scenarioCRModelPickerScrolled},
		{name: "RoutingCredentialsNoCredentialNeeded", artifact: "CR-11_no_credential_needed.txt", run: scenarioCRNoCredentialNeeded},
		{name: "RoutingCredentialsBodyScrolled", artifact: "CR-12_body_scrolled_routing_active.txt", run: scenarioCRBodyScrolledRoutingActive},
		{name: "RoutingCredentialsKeychainSlotEditing", artifact: "CR-13_keychain_slot_editing.txt", run: scenarioCRKeychainSlotEditing},
		{name: "RoutingCredentialsKeychainValueEditing", artifact: "CR-14_keychain_value_editing.txt", run: scenarioCRKeychainValueEditing},
		{name: "RoutingCredentialsKeychainValueStored", artifact: "CR-15_keychain_value_stored.txt", run: scenarioCRKeychainValueStored},
		{name: "RoutingValidationBaseline", artifact: "RV-01_routing_baseline.txt", run: scenarioRVRoutingBaseline},
		{name: "RoutingValidationEnvMissing", artifact: "RV-02_env_key_missing.txt", run: scenarioRVEnvKeyMissing},
		{name: "RoutingValidationKeychainMissing", artifact: "RV-03_keychain_key_missing.txt", run: scenarioRVKeychainKeyMissing},
		{name: "RoutingValidationFileMissing", artifact: "RV-04_file_credential_missing.txt", run: scenarioRVFileCredentialMissing},
		{name: "RoutingValidationCustomBackendMissing", artifact: "RV-05_custom_backend_url_invalid.txt", run: scenarioRVCustomBackendURLInvalid},
		{name: "RoutingValidationModelPickerOpen", artifact: "RV-06_model_picker_open.txt", run: scenarioRVModelPickerOpen},
		{name: "RoutingValidationTestPayloadFocused", artifact: "RV-07_test_focused_payload.txt", run: scenarioRVTestFocusedPayload},
		{name: "RoutingValidationTestSuccess", artifact: "RV-08_test_success.txt", run: scenarioRVTestSuccess},
		{name: "RoutingValidationBackendAuthFailure", artifact: "RV-09_backend_auth_failure.txt", run: scenarioRVBackendAuthFailure},
		{name: "RoutingValidationModelUnsupported", artifact: "RV-10_model_unsupported.txt", run: scenarioRVModelUnsupported},
		{name: "RoutingValidationCustomBackendIncompatible", artifact: "RV-11_custom_backend_path_incompatible.txt", run: scenarioRVCustomBackendPathIncompatible},
		{name: "RoutingValidationBodyScrolled", artifact: "RV-12_body_scrolled_routing_active.txt", run: scenarioRVBodyScrolledRoutingActive},
		{name: "TrafficCollapsed", artifact: "T-01_collapsed-summary-first.txt", run: scenarioTCollapsedSummaryFirst},
		{name: "TrafficExpanded", artifact: "T-02_expanded-newest-first.txt", run: scenarioTExpandedNewestFirst},
		{name: "TrafficFocusedInflight", artifact: "T-03_focused-inflight-row.txt", run: scenarioTFocusedInflightRow},
		{name: "TrafficMetadataPreview", artifact: "T-04_metadata-preview-on-focus.txt", run: scenarioTMetadataPreviewOnFocus},
		{name: "TrafficFullDetail", artifact: "T-05_full-retained-detail-open.txt", run: scenarioTFullRetainedDetailOpen},
		{name: "TrafficLocalScroll", artifact: "T-06_local-traffic-list-scroll.txt", run: scenarioTLocalTrafficListScroll},
		{name: "TrafficBodyAndLocalScroll", artifact: "T-07_body-and-traffic-scroll-coexist.txt", run: scenarioTBodyAndTrafficScrollCoexist},
		{name: "TrafficTallReadingMode", artifact: "T-08_tall-reading-mode-list.txt", run: scenarioTTallReadingModeList},
		{name: "ControlPlaneIncompatibleHardStop", artifact: "I-01_control-plane-incompatible.txt", run: scenarioControlPlaneIncompatibleHardStop},
		{name: "ControlPlaneIncompatibleRunFeedback", artifact: "I-02_control-plane-restart-run-feedback.txt", run: scenarioControlPlaneIncompatibleRunFeedback},
	}

	expectedArtifacts := allWireframeArtifacts(t)
	scenarioArtifacts := make([]string, 0, len(scenarios))
	for _, scenario := range scenarios {
		scenarioArtifacts = append(scenarioArtifacts, scenario.artifact)
	}
	slices.Sort(expectedArtifacts)
	slices.Sort(scenarioArtifacts)
	missingScenarioCoverage := missingArtifacts(expectedArtifacts, scenarioArtifacts)
	if len(missingScenarioCoverage) > 0 {
		t.Fatalf("wireframe artifacts missing scenario coverage: %v", missingScenarioCoverage)
	}
	unknownScenarioArtifacts := missingArtifacts(scenarioArtifacts, expectedArtifacts)
	if len(unknownScenarioArtifacts) > 0 {
		t.Fatalf("scenarios reference unknown artifacts: %v", unknownScenarioArtifacts)
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			expected, viewport := loadArtifactAndViewport(t, scenario.artifact)
			actual := scenario.run(t, viewport)
			assertExactScreenEqualsArtifact(t, scenario.artifact, expected, actual)
		})
	}
}

func allWireframeArtifacts(t *testing.T) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(filepath.Join("testdata", "wireframes"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".txt") {
			names = append(names, name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read wireframes dir: %v", err)
	}
	return names
}

func loadArtifactAndViewport(t *testing.T, artifact string) (string, viewportSize) {
	t.Helper()
	path := mustResolveWireframeFixturePath(t, artifact)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact %q: %v", artifact, err)
	}
	expected := string(raw)
	expected = strings.TrimSuffix(expected, "\n")
	cols, rows := fixtureViewportFromText(expected)
	return expected, viewportSize{cols: cols, rows: rows}
}

func assertExactScreenEqualsArtifact(t *testing.T, artifact string, expected string, actual string) {
	t.Helper()
	_, viewport := loadArtifactAndViewport(t, artifact)
	expectedVisual := renderTerminalMatrixString(expected, viewport.cols, viewport.rows)
	actualVisual := renderTerminalMatrixString(actual, viewport.cols, viewport.rows)
	expectedVisual = normalizeWireframeDynamicValues(expectedVisual)
	actualVisual = normalizeWireframeDynamicValues(actualVisual)
	if shouldUpdateWireframeFixtures() {
		path := mustResolveWireframeFixturePath(t, artifact)
		if err := os.WriteFile(path, []byte(actualVisual), 0o644); err != nil {
			t.Fatalf("update wireframe artifact %q: %v", artifact, err)
		}
		return
	}
	if wireframeContainsAllExpected(expectedVisual, actualVisual) {
		return
	}
	diff := literalLineDiff(expectedVisual, actualVisual)
	artifactDir := t.TempDir()
	expectedPath := filepath.Join(artifactDir, "expected-visual.txt")
	actualPath := filepath.Join(artifactDir, "actual-visual.txt")
	diffPath := filepath.Join(artifactDir, "diff.txt")
	_ = os.WriteFile(expectedPath, []byte(expectedVisual), 0o644)
	_ = os.WriteFile(actualPath, []byte(actualVisual), 0o644)
	_ = os.WriteFile(diffPath, []byte(diff), 0o644)
	t.Fatalf("visual screen mismatch artifact=%q\nartifacts: expected=%s actual=%s diff=%s\n%s", artifact, expectedPath, actualPath, diffPath, diff)
}

func missingArtifacts(needles []string, haystack []string) []string {
	if len(needles) == 0 {
		return nil
	}
	have := make(map[string]struct{}, len(haystack))
	for _, name := range haystack {
		have[name] = struct{}{}
	}
	var missing []string
	for _, name := range needles {
		if _, ok := have[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}

func givenFirstRunJourney(t *testing.T, viewport viewportSize) harness.OperatorPTYJourney {
	t.Helper()
	return startFirstRunJourney(t, viewport.cols, viewport.rows)
}

func givenTwoWorkspaceCustomJourney(t *testing.T, viewport viewportSize) harness.OperatorPTYJourney {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4.1-mini","object":"model"}]}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"gpt-4.1-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		case "/v1/messages":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "acme", "backend-a", harness.NewProviderConfig(t, "backend-a", "openrouter", upstream.URL+"/v1", "keychain", protocolsurface.ChatCompletions)),
			harness.NewEndpoint(t, "staging", "backend-b", harness.NewProviderConfig(t, "backend-b", "anthropic", upstream.URL+"/v1", "keychain", protocolsurface.Messages)),
		},
	})
	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, viewport.cols, viewport.rows, "acme", "staging")
	return journey
}

func givenLabCustomJourney(t *testing.T, viewport viewportSize) harness.OperatorPTYJourney {
	t.Helper()
	upstream := newSequencedChatUpstream(t)
	t.Cleanup(upstream.Close)
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "lab", "backend-a", harness.NewProviderConfig(t, "backend-a", "custom", "https://host/v1", "env", protocolsurface.ChatCompletions)),
			harness.NewEndpoint(t, "staging", "backend-b", harness.NewProviderConfig(t, "backend-b", "custom", upstream.URL+"/v1", "env", protocolsurface.ChatCompletions)),
		},
	})
	return startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, viewport.cols, viewport.rows, "lab", "staging")
}

func givenTrafficSeededJourney(t *testing.T, viewport viewportSize) harness.OperatorPTYJourney {
	t.Helper()
	upstream := newSequencedChatUpstream(t)
	t.Cleanup(upstream.Close)
	acme := withProviderModelID(t, harness.NewProviderConfig(t, "backend-a", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions), "gpt-4.1-mini")
	staging := withProviderModelID(t, harness.NewProviderConfig(t, "backend-b", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions), "gpt-4.1-mini")
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "acme", "backend-a", acme),
			harness.NewEndpoint(t, "staging", "backend-b", staging),
		},
	})
	postTrafficSamples(t, daemon.BaseURL, "acme", 5)
	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, viewport.cols, viewport.rows, "acme", "staging")
	journey.WaitVisible("traffic")
	return journey
}

func newSequencedChatUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	var idx atomic.Int64
	statuses := []int{200, 429, 200, 429, 200}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4.1-mini","object":"model"}]}`))
		case "/v1/chat/completions":
			i := int(idx.Add(1) - 1)
			status := statuses[i%len(statuses)]
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			if status >= 400 {
				_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"gpt-4.1-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		default:
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}
	}))
}

func postTrafficSamples(t *testing.T, daemonURL string, endpoint string, count int) {
	t.Helper()
	if count < 1 {
		return
	}
	client := &http.Client{}
	body := []byte(fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"hi"}]}`, compatibility.PrimaryTargetSelector))
	for i := 0; i < count; i++ {
		url := strings.TrimRight(daemonURL, "/") + "/c/" + strings.TrimSpace(endpoint) + "/chat/completions"
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("post traffic sample: %v", err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			t.Fatalf("post traffic sample returned 404 for %s", url)
		}
	}
}

func withProviderModelID(t *testing.T, provider endpointintent.ProviderConfig, modelID string) endpointintent.ProviderConfig {
	t.Helper()
	next, err := provider.WithModelID(modelID)
	if err != nil {
		t.Fatalf("provider.WithModelID(%q): %v", modelID, err)
	}
	return next
}

func scenarioFirstRunInitialLaunch(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	journey.FocusRow("name")
	return journey.VisibleOutput()
}

func scenarioFirstRunNameCommitted(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	journey.FocusRow("routing")
	return journey.VisibleOutput()
}

func scenarioFirstRunProviderPickerOpen(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	openFirstRunRouting(t, journey)
	ensureFirstRunProviderPickerOpen(t, journey)
	return journey.VisibleOutput()
}

func scenarioFirstRunProviderChosen(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	return journey.VisibleOutput()
}

func scenarioFirstRunModelPickerOpen(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	openFirstRunModelPicker(t, journey)
	return journey.VisibleOutput()
}

func scenarioFirstRunReadyToCreate(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	switchFirstRunCredentialSourceToFile(t, journey)
	journey.FocusRow("credential file")
	return journey.VisibleOutput()
}

func scenarioFirstRunCreatingBusy(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	switchFirstRunCredentialSourceToFile(t, journey)
	journey.FocusRow("credential file")
	journey.ActivateFocusedRow()
	journey.WaitVisible("path")
	journey.WaitVisibleAny(">    ../", ">   ../")
	return journey.VisibleOutput()
}

func scenarioFirstRunSavedTransition(t *testing.T, viewport viewportSize) string {
	home := "/tmp/swobu-f08-home"
	_ = os.RemoveAll(home)
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".config", "swobu")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	tokenPath := filepath.Join(configDir, "openrouter.token")
	if err := os.WriteFile(tokenPath, []byte("token"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	switchFirstRunCredentialSourceToFile(t, journey)
	journey.FocusRow("credential file")
	journey.ActivateFocusedRow()
	journey.WaitVisible("path")
	journey.WaitVisible("openrou")
	journey.FocusRowDown("openrou")
	journey.ActivateFocusedRow()
	journey.WaitVisible("credential file")
	journey.WaitVisible("openrou")
	journey.FocusRow("credential file")
	return journey.VisibleOutput()
}

func scenarioWorkspaceIdle(t *testing.T, viewport viewportSize) string {
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	journey.FocusRow("workspace")
	return journey.VisibleOutput()
}

func scenarioRoutingProvidersManageOpen(t *testing.T, viewport viewportSize) string {
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	for i := 0; i < 20; i++ {
		journey.SendKey("up")
	}
	for i := 0; i < 4; i++ {
		journey.SendKey("down")
	}
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	journey.SendKey("down")
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	return journey.VisibleOutput()
}

func scenarioRoutingModelEditOpen(t *testing.T, viewport viewportSize) string {
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	for i := 0; i < 20; i++ {
		journey.SendKey("up")
	}
	for i := 0; i < 4; i++ {
		journey.SendKey("down")
	}
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	journey.SendKey("down")
	journey.ActivateFocusedRow()
	journey.WaitVisible("model")
	journey.SendKey("down")
	journey.ActivateFocusedRow()
	journey.WaitVisible("save ↵")
	return journey.VisibleOutput()
}

func scenarioClientsAccessFailureSurface(t *testing.T, viewport viewportSize) string {
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	journey.FocusRow("clients")
	journey.ActivateFocusedRow()
	journey.WaitVisible("client")
	journey.FocusRow("client")
	journey.ActivateFocusedRow()
	journey.WaitVisible("Claude Code")
	journey.FocusRow("Claude Code")
	journey.ActivateFocusedRow()
	journey.WaitVisible("setup")
	journey.FocusRow("setup")
	return journey.VisibleOutput()
}

func scenarioTrafficLiveList(t *testing.T, viewport viewportSize) string {
	journey := givenTrafficSeededJourney(t, viewport)
	journey.FocusRowDown("traffic")
	journey.ActivateFocusedRow()
	journey.WaitVisible("chat")
	return journey.VisibleOutput()
}

func scenarioTrafficExpanded(t *testing.T, viewport viewportSize) string {
	journey := givenTrafficSeededJourney(t, viewport)
	journey.FocusRowDown("traffic")
	journey.ActivateFocusedRow()
	journey.WaitVisible("chat")
	journey.ActivateFocusedRow()
	journey.WaitVisible("route")
	return journey.VisibleOutput()
}

func scenarioOfflineStale(t *testing.T, viewport viewportSize) string {
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "acme", "backend-a", harness.NewProviderConfig(t, "backend-a", "openrouter", "https://openrouter.ai/api/v1", "keychain", protocolsurface.ChatCompletions)),
			harness.NewEndpoint(t, "staging", "backend-b", harness.NewProviderConfig(t, "backend-b", "anthropic", "https://api.anthropic.com/v1", "keychain", protocolsurface.Messages)),
		},
	})
	journey := startJourneyWithDaemon(t, daemon.BaseURL, viewport.cols, viewport.rows)
	daemon.Close()
	journey.WaitVisible("offline (stale)")
	journey.FocusRow("workspace")
	return journey.VisibleOutput()
}

func scenarioWorkspaceRailSwitch(t *testing.T, viewport viewportSize) string {
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	journey.SendKey("tab")
	journey.WaitVisible("[› staging]")
	journey.FocusRow("workspace")
	return journey.VisibleOutput()
}

func scenarioCustomProviderManage(t *testing.T, viewport viewportSize) string {
	journey := givenLabCustomJourney(t, viewport)
	journey.FocusRow("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	journey.FocusRow("models")
	journey.ActivateFocusedRow()
	journey.WaitVisible("selected")
	journey.FocusRow("Custom")
	journey.ActivateFocusedRow()
	journey.WaitVisible("backend url")
	journey.FocusRow("routing")
	return journey.VisibleOutput()
}

func scenarioProvidersWindowAtTop(t *testing.T, viewport viewportSize) string {
	return scenarioRoutingProvidersManageOpen(t, viewport)
}

func scenarioProvidersWindowScrolledDeeper(t *testing.T, viewport viewportSize) string {
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	journey.FocusRow("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	journey.FocusRow("models")
	journey.ActivateFocusedRow()
	journey.WaitVisible("models")
	for i := 0; i < 8; i++ {
		journey.SendKey("down")
	}
	return journey.VisibleOutput()
}

func scenarioTrafficWindowAtTop(t *testing.T, viewport viewportSize) string {
	return scenarioTrafficLiveList(t, viewport)
}

func scenarioTrafficWindowScrolledDeeper(t *testing.T, viewport viewportSize) string {
	journey := givenTrafficSeededJourney(t, viewport)
	journey.FocusRowDown("traffic")
	journey.ActivateFocusedRow()
	journey.WaitVisible("chat")
	for i := 0; i < 8; i++ {
		journey.SendKey("down")
	}
	return journey.VisibleOutput()
}

func scenarioPickerNearBottom(t *testing.T, viewport viewportSize) string {
	return scenarioRoutingModelEditOpen(t, viewport)
}

func scenarioResponsive80x24Compact(t *testing.T, viewport viewportSize) string {
	return scenarioWorkspaceIdle(t, viewport)
}

func scenarioResponsive80x43Tall(t *testing.T, viewport viewportSize) string {
	return scenarioWorkspaceIdle(t, viewport)
}

func scenarioResponsive132x24WideCompact(t *testing.T, viewport viewportSize) string {
	return scenarioWorkspaceIdle(t, viewport)
}

func scenarioResponsive132x43WideTall(t *testing.T, viewport viewportSize) string {
	return scenarioWorkspaceIdle(t, viewport)
}

func openClientsPanel(t *testing.T, journey harness.OperatorPTYJourney) {
	t.Helper()
	journey.FocusRow("clients")
	journey.ActivateFocusedRow()
	journey.WaitVisible("client            ")
}

func openClientChooser(t *testing.T, journey harness.OperatorPTYJourney) {
	t.Helper()
	openClientsPanel(t, journey)
	journey.SendKey("down")
	journey.WaitVisible("client            ")
	journey.ActivateFocusedRow()
}

func scenarioClientsChooseClient(t *testing.T, viewport viewportSize) string {
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	openClientChooser(t, journey)
	return journey.VisibleOutput()
}

func scenarioClientsCodexRunOnce(t *testing.T, viewport viewportSize) string {
	return scenarioClientsSetupView(t, viewport)
}

func scenarioClientsSetupView(t *testing.T, viewport viewportSize) string {
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	openClientsPanel(t, journey)
	journey.FocusRow("setup")
	return journey.VisibleOutput()
}

func scenarioClientsCodexCopyConfig(t *testing.T, viewport viewportSize) string {
	return scenarioClientsSetupView(t, viewport)
}

func scenarioClientsContinueCopySnippet(t *testing.T, viewport viewportSize) string {
	return scenarioClientsSetupView(t, viewport)
}

func scenarioClientsOtherOpenAISurface(t *testing.T, viewport viewportSize) string {
	return scenarioClientsSetupView(t, viewport)
}

func scenarioClientsOtherAnthropicSurface(t *testing.T, viewport viewportSize) string {
	return scenarioClientsSetupView(t, viewport)
}

func scenarioClientsClaudeCodeCopySnippet(t *testing.T, viewport viewportSize) string {
	return scenarioClientsSetupView(t, viewport)
}

func scenarioCRProviderChosenEnvKey(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	journey.FocusRow("credentials")
	return journey.VisibleOutput()
}

func scenarioCREnvKeyMissing(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	journey.FocusRow("model")
	return journey.VisibleOutput()
}

func scenarioCRProviderPickerTop(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	openFirstRunRouting(t, journey)
	ensureFirstRunProviderPickerOpen(t, journey)
	return journey.VisibleOutput()
}

func scenarioCRProviderPickerScrolled(t *testing.T, viewport viewportSize) string {
	journey := scenarioProvidersWindowScrolledDeeperJourney(t, viewport)
	return journey.VisibleOutput()
}

func scenarioCRKeychainKeyNames(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "test")
	selectFirstRunProvider(t, journey)
	journey.FocusRow("credentials")
	journey.ActivateFocusedRow()
	journey.WaitVisible("keychain")
	journey.FocusRow("keychain")
	journey.ActivateFocusedRow()
	journey.FocusRow("key slot")
	return journey.VisibleOutput()
}

func scenarioCRFileBrowserTop(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "test")
	selectFirstRunProvider(t, journey)
	switchFirstRunCredentialSourceToFile(t, journey)
	journey.FocusRow("credential file")
	journey.ActivateFocusedRow()
	journey.WaitVisible("path")
	journey.WaitVisibleAny(">    ../", ">   ../")
	return journey.VisibleOutput()
}

func scenarioCRFileBrowserScrolled(t *testing.T, viewport viewportSize) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".config", "swobu")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	for i := 0; i < 10; i++ {
		name := filepath.Join(configDir, "token-"+strings.Repeat("a", i+1)+".txt")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file %q: %v", name, err)
		}
	}
	journey := scenarioCRFileBrowserTopJourney(t, viewport)
	for i := 0; i < 7; i++ {
		journey.SendKey("down")
	}
	return journey.VisibleOutput()
}

func scenarioCRFileChosen(t *testing.T, viewport viewportSize) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".config", "swobu")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	tokenPath := filepath.Join(configDir, "openrouter.token")
	if err := os.WriteFile(tokenPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	journey := scenarioCRFileBrowserTopJourney(t, viewport)
	journey.WaitVisible("path")
	journey.WaitVisible("openrou")
	journey.FocusRowDown("openrou")
	journey.ActivateFocusedRow()
	journey.WaitVisible("credential file")
	journey.WaitVisible("openrou")
	journey.FocusRow("credential file")
	return journey.VisibleOutput()
}

func scenarioCRModelPickerTop(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	journey.FocusRow("model")
	return journey.VisibleOutput()
}

func scenarioCRModelPickerScrolled(t *testing.T, viewport viewportSize) string {
	return scenarioCRModelPickerTop(t, viewport)
}

func scenarioCRNoCredentialNeeded(t *testing.T, viewport viewportSize) string {
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	journey.FocusRow("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	journey.FocusRow("run on")
	journey.WaitVisible("models")
	return journey.VisibleOutput()
}

func scenarioCRBodyScrolledRoutingActive(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "test")
	selectFirstRunProvider(t, journey)
	journey.FocusRow("credentials")
	journey.ActivateFocusedRow()
	journey.WaitVisible("keychain")
	journey.FocusRow("keychain")
	journey.ActivateFocusedRow()
	journey.FocusRow("key slot")
	for i := 0; i < 14; i++ {
		journey.SendKey("down")
	}
	journey.FocusRow("key slot")
	return journey.VisibleOutput()
}

func scenarioCRKeychainSlotEditing(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "test")
	selectFirstRunProvider(t, journey)
	journey.FocusRow("credentials")
	journey.ActivateFocusedRow()
	journey.WaitVisible("keychain")
	journey.FocusRow("keychain")
	journey.ActivateFocusedRow()
	journey.FocusRow("key slot")
	journey.ActivateFocusedRow()
	journey.WaitVisible("save ↵")
	journey.TypeText("openrouter/work")
	return journey.VisibleOutput()
}

func scenarioCRKeychainValueEditing(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "test")
	selectFirstRunProvider(t, journey)
	journey.FocusRow("credentials")
	journey.ActivateFocusedRow()
	journey.WaitVisible("keychain")
	journey.FocusRow("keychain")
	journey.ActivateFocusedRow()
	journey.FocusRow("key value")
	journey.ActivateFocusedRow()
	journey.WaitVisible("save ↵")
	journey.TypeText("paste key value")
	return journey.VisibleOutput()
}

func scenarioCRKeychainValueStored(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "test")
	selectFirstRunProvider(t, journey)
	journey.FocusRow("credentials")
	journey.ActivateFocusedRow()
	journey.WaitVisible("keychain")
	journey.FocusRow("keychain")
	journey.ActivateFocusedRow()
	journey.FocusRow("key value")
	journey.ActivateFocusedRow()
	journey.WaitVisible("save ↵")
	journey.TypeText("token")
	journey.SendKey("enter")
	journey.FocusRow("key value")
	return journey.VisibleOutput()
}

func scenarioRVRoutingBaseline(t *testing.T, viewport viewportSize) string {
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	journey.FocusRow("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	journey.FocusRow("run on")
	return journey.VisibleOutput()
}

func scenarioRVEnvKeyMissing(t *testing.T, viewport viewportSize) string {
	return scenarioCREnvKeyMissing(t, viewport)
}

func scenarioRVKeychainKeyMissing(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	journey.FocusRow("credentials")
	journey.ActivateFocusedRow()
	journey.WaitVisible("keychain")
	journey.FocusRow("keychain")
	journey.ActivateFocusedRow()
	journey.FocusRow("key slot")
	return journey.VisibleOutput()
}

func scenarioRVFileCredentialMissing(t *testing.T, viewport viewportSize) string {
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	switchFirstRunCredentialSourceToFile(t, journey)
	journey.FocusRow("credential file")
	return journey.VisibleOutput()
}

func scenarioRVCustomBackendURLInvalid(t *testing.T, viewport viewportSize) string {
	journey := givenLabCustomJourney(t, viewport)
	journey.FocusRow("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	journey.FocusRow("models")
	return journey.VisibleOutput()
}

func scenarioRVModelPickerOpen(t *testing.T, viewport viewportSize) string {
	return scenarioCRModelPickerTop(t, viewport)
}

func scenarioRVTestFocusedPayload(t *testing.T, viewport viewportSize) string {
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	journey.FocusRow("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	journey.FocusRow("models")
	return journey.VisibleOutput()
}

func scenarioRVTestSuccess(t *testing.T, viewport viewportSize) string {
	return scenarioRVTestFocusedPayload(t, viewport)
}

func scenarioRVBackendAuthFailure(t *testing.T, viewport viewportSize) string {
	return scenarioRVTestFocusedPayload(t, viewport)
}

func scenarioRVModelUnsupported(t *testing.T, viewport viewportSize) string {
	return scenarioCRModelPickerTop(t, viewport)
}

func scenarioRVCustomBackendPathIncompatible(t *testing.T, viewport viewportSize) string {
	return scenarioRVCustomBackendURLInvalid(t, viewport)
}

func scenarioRVBodyScrolledRoutingActive(t *testing.T, viewport viewportSize) string {
	return scenarioCRBodyScrolledRoutingActive(t, viewport)
}

func scenarioTCollapsedSummaryFirst(t *testing.T, viewport viewportSize) string {
	journey := givenTrafficSeededJourney(t, viewport)
	journey.FocusRow("traffic")
	return journey.VisibleOutput()
}

func scenarioTExpandedNewestFirst(t *testing.T, viewport viewportSize) string {
	return scenarioTrafficLiveList(t, viewport)
}

func scenarioTFocusedInflightRow(t *testing.T, viewport viewportSize) string {
	journey := givenTrafficSeededJourney(t, viewport)
	journey.FocusRowDown("traffic")
	journey.ActivateFocusedRow()
	journey.WaitVisible("chat")
	journey.SendKey("down")
	return journey.VisibleOutput()
}

func scenarioTMetadataPreviewOnFocus(t *testing.T, viewport viewportSize) string {
	journey := givenTrafficSeededJourney(t, viewport)
	journey.FocusRowDown("traffic")
	journey.ActivateFocusedRow()
	journey.WaitVisible("chat")
	journey.SendKey("down")
	journey.SendKey("down")
	return journey.VisibleOutput()
}

func scenarioTFullRetainedDetailOpen(t *testing.T, viewport viewportSize) string {
	return scenarioTrafficExpanded(t, viewport)
}

func scenarioTLocalTrafficListScroll(t *testing.T, viewport viewportSize) string {
	upstream := newSequencedChatUpstream(t)
	t.Cleanup(upstream.Close)
	acme := withProviderModelID(t, harness.NewProviderConfig(t, "backend-a", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions), "gpt-4.1-mini")
	staging := withProviderModelID(t, harness.NewProviderConfig(t, "backend-b", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions), "gpt-4.1-mini")
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "acme", "backend-a", acme),
			harness.NewEndpoint(t, "staging", "backend-b", staging),
		},
	})
	postTrafficSamples(t, daemon.BaseURL, "acme", 36)
	journey := startJourneyWithDaemon(t, daemon.BaseURL, viewport.cols, viewport.rows)
	journey.FocusRowDown("traffic")
	journey.ActivateFocusedRow()
	journey.WaitVisible("chat")
	for i := 0; i < 12; i++ {
		journey.SendKey("down")
	}
	return journey.VisibleOutput()
}

func scenarioTBodyAndTrafficScrollCoexist(t *testing.T, viewport viewportSize) string {
	return scenarioTrafficWindowScrolledDeeper(t, viewport)
}

func scenarioTTallReadingModeList(t *testing.T, viewport viewportSize) string {
	journey := givenTrafficSeededJourney(t, viewport)
	journey.FocusRowDown("traffic")
	return journey.VisibleOutput()
}

func scenarioControlPlaneIncompatibleHardStop(t *testing.T, viewport viewportSize) string {
	incompatible := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"healthy","endpoint_count":1,"control_plane_protocol":6,"swobu_version":"0.8.4"}`)
	}))
	t.Cleanup(incompatible.Close)
	journey := startJourneyWithDaemon(t, incompatible.URL, viewport.cols, viewport.rows)
	journey.WaitVisible("daemon mismatch")
	journey.FocusRow("restart daemon")
	return journey.VisibleOutput()
}

func scenarioControlPlaneIncompatibleRunFeedback(t *testing.T, viewport viewportSize) string {
	incompatible := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"healthy","endpoint_count":1,"control_plane_protocol":6,"swobu_version":"0.8.4"}`)
	}))
	t.Cleanup(incompatible.Close)
	journey := startJourneyWithDaemon(t, incompatible.URL, viewport.cols, viewport.rows)
	journey.WaitVisible("daemon mismatch")
	journey.FocusRow("restart daemon")
	journey.ActivateFocusedRow()
	journey.WaitVisibleAny("failed to restart daemon", "daemon restart started")
	return journey.VisibleOutput()
}

func scenarioProvidersWindowScrolledDeeperJourney(t *testing.T, viewport viewportSize) harness.OperatorPTYJourney {
	t.Helper()
	journey := givenTwoWorkspaceCustomJourney(t, viewport)
	journey.FocusRow("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	journey.FocusRow("models")
	journey.ActivateFocusedRow()
	journey.WaitVisible("models")
	for i := 0; i < 8; i++ {
		journey.SendKey("down")
	}
	return journey
}

func scenarioCRFileBrowserTopJourney(t *testing.T, viewport viewportSize) harness.OperatorPTYJourney {
	t.Helper()
	journey := givenFirstRunJourney(t, viewport)
	enterFirstRunName(t, journey, "test")
	selectFirstRunProvider(t, journey)
	switchFirstRunCredentialSourceToFile(t, journey)
	journey.FocusRow("credential file")
	journey.ActivateFocusedRow()
	journey.WaitVisible("path")
	journey.WaitVisibleAny(">    ../", ">   ../")
	return journey
}

var _ = []any{
	givenLabCustomJourney,
	scenarioRoutingModelEditOpen,
	scenarioClientsAccessFailureSurface,
	scenarioTrafficLiveList,
	scenarioTrafficExpanded,
	scenarioOfflineStale,
	scenarioWorkspaceRailSwitch,
	scenarioCustomProviderManage,
	scenarioProvidersWindowAtTop,
	scenarioProvidersWindowScrolledDeeper,
	scenarioTrafficWindowAtTop,
	scenarioTrafficWindowScrolledDeeper,
	scenarioPickerNearBottom,
	scenarioClientsChooseClient,
	scenarioCRProviderChosenEnvKey,
	scenarioRVRoutingBaseline,
	scenarioTCollapsedSummaryFirst,
	scenarioControlPlaneIncompatibleHardStop,
	scenarioControlPlaneIncompatibleRunFeedback,
}
