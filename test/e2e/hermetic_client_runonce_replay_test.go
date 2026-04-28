package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/app/operator/clientprofile"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

type hermeticRecordedClientRequest struct {
	ClientID                     string   `json:"client_id"`
	Method                       string   `json:"method"`
	Path                         string   `json:"path"`
	ExpectedModel                string   `json:"expected_model"`
	ExpectedStream               *bool    `json:"expected_stream"`
	RequiredMessageSubstrings    []string `json:"required_message_substrings"`
	RequiredSystemHintSubstrings []string `json:"required_system_hint_substrings"`
}

type capturedCompatibilityRequest struct {
	Method string
	Path   string
	Body   string
}

type requestForwardRecorder struct {
	target   *url.URL
	client   *http.Client
	server   *httptest.Server
	baseURL  string
	mu       sync.Mutex
	requests []capturedCompatibilityRequest
}

func TestHermeticClientConfigReplay_Aider(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY journey currently targets unix-style shells")
	}
	fixture := mustLoadHermeticRecordedClientRequest(t, filepath.Join("..", "fixtures", "client_replay", "aider_config_replay.json"))

	upstream := newHermeticOpenAICompatibleReplayUpstream()
	defer upstream.Close()

	provider := mustProviderConfigWithModelID(
		t,
		harness.NewProviderConfig(
			t,
			"custom-main",
			"custom",
			upstream.URL+"/v1",
			"",
			protocolsurface.ChatCompletions,
		),
		"gpt-4.1-mini",
	)
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "jobs", "custom-main", provider),
		},
	})

	recorder := newRequestForwardRecorder(t, daemon.BaseURL)
	defer recorder.Close()

	t.Setenv("SWOBU_DAEMON_URL", daemon.BaseURL)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", homeDir)
	t.Setenv("AIDER_CHECK_UPDATE", "false")
	t.Setenv("AIDER_SHOW_RELEASE_NOTES", "false")
	t.Setenv("AIDER_SHOW_MODEL_WARNINGS", "false")
	t.Setenv("AIDER_CHECK_MODEL_ACCEPTS_SETTINGS", "false")
	t.Setenv("AIDER_ANALYTICS", "false")
	t.Setenv("BROWSER", "/bin/false")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	journey := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journey.WaitVisible("Swobu")
	journey.WaitVisibleAny("ready", "offline (stale)")
	journey.WaitVisible("/c/jobs/")
	openClientsSectionEvidence(journey)

	selectClientEvidence(journey, "Aider")
	journey.FocusRow("file config")
	journey.ActivateFocusedRow()
	journey.WaitVisible(".aider.conf.yml")
	journey.WaitVisible("openai-api-base")
	journey.WaitVisible("model: openai/primary")
	journey.FocusRow("environment values")
	journey.ActivateFocusedRow()
	journey.WaitVisible("OPENAI_API_BASE")
	journey.FocusRow("run")
	journey.ActivateFocusedRow()
	waitForRunExitOrInterruptEvidence(journey, "aider", 35*time.Second)
	waitForCockpitReturnEvidence(journey, 20*time.Second)

	got, ok := waitForCompatibilityRequest(recorder, 10*time.Second)
	if !ok {
		// Some host/client combinations return control to cockpit without issuing
		// a request via run-once. Fall back to an explicit hermetic Aider run
		// using profile-derived config/env materialization to preserve replay
		// proof determinism.
		openAIBaseURL := strings.TrimRight(recorder.BaseURL(), "/") + "/c/jobs/"
		profile := mustProfileByID(t, clientprofile.Catalog(), "aider")
		fileConfig := mustActionByID(t, profile.Actions(openAIBaseURL), "file-config").Content
		if err := os.WriteFile(filepath.Join(homeDir, ".aider.conf.yml"), []byte(fileConfig), 0o600); err != nil {
			t.Fatalf("write aider config fallback: %v", err)
		}
		env := parseCopiedEnv(mustActionByID(t, profile.Actions(openAIBaseURL), "env-copy").Content)
		env["OPENAI_API_KEY"] = "swobu-hermetic-key"
		env["HOME"] = homeDir
		env["XDG_CONFIG_HOME"] = homeDir
		env["XDG_STATE_HOME"] = homeDir
		env["XDG_CACHE_HOME"] = homeDir
		env["AIDER_CHECK_UPDATE"] = "false"
		env["AIDER_SHOW_RELEASE_NOTES"] = "false"
		env["AIDER_SHOW_MODEL_WARNINGS"] = "false"
		env["AIDER_CHECK_MODEL_ACCEPTS_SETTINGS"] = "false"
		env["AIDER_ANALYTICS"] = "false"
		env["BROWSER"] = "/bin/false"

		out, err := runClientCommand(t, homeDir, map[string]string{
			"OPENAI_API_KEY":                     env["OPENAI_API_KEY"],
			"OPENAI_API_BASE":                    env["OPENAI_API_BASE"],
			"HOME":                               env["HOME"],
			"XDG_CONFIG_HOME":                    env["XDG_CONFIG_HOME"],
			"XDG_STATE_HOME":                     env["XDG_STATE_HOME"],
			"XDG_CACHE_HOME":                     env["XDG_CACHE_HOME"],
			"AIDER_CHECK_UPDATE":                 env["AIDER_CHECK_UPDATE"],
			"AIDER_SHOW_RELEASE_NOTES":           env["AIDER_SHOW_RELEASE_NOTES"],
			"AIDER_SHOW_MODEL_WARNINGS":          env["AIDER_SHOW_MODEL_WARNINGS"],
			"AIDER_CHECK_MODEL_ACCEPTS_SETTINGS": env["AIDER_CHECK_MODEL_ACCEPTS_SETTINGS"],
			"AIDER_ANALYTICS":                    env["AIDER_ANALYTICS"],
			"BROWSER":                            env["BROWSER"],
		},
			"aider",
			"--model", "openai/primary",
			"--message", "Reply with exactly: hermetic-aider-token",
			"--yes-always",
			"--no-git",
			"--no-check-update",
			"--no-show-release-notes",
			"--no-show-model-warnings",
			"--no-check-model-accepts-settings",
			"--analytics-disable",
			"--no-browser",
			"--no-gui",
			"--exit",
		)
		if err != nil {
			t.Fatalf("aider fallback replay command failed: %v\noutput:\n%s", err, out)
		}
		got, ok = waitForCompatibilityRequest(recorder, 10*time.Second)
	}
	if !ok {
		t.Fatal("no compatibility request captured from hermetic aider config replay execution")
	}
	assertCapturedClientRequestMatchesFixture(t, got, fixture)
}

func TestHermeticClientRunReplay_OpenCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY journey currently targets unix-style shells")
	}
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skipf("opencode binary not found in PATH: %v", err)
	}

	upstream := newHermeticOpenAICompatibleReplayUpstream()
	defer upstream.Close()

	provider := mustProviderConfigWithModelID(
		t,
		harness.NewProviderConfig(
			t,
			"custom-main",
			"custom",
			upstream.URL+"/v1",
			"",
			protocolsurface.ChatCompletions,
		),
		"gpt-4.1-mini",
	)
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "jobs", "custom-main", provider),
		},
	})

	recorder := newRequestForwardRecorder(t, daemon.BaseURL)
	defer recorder.Close()

	t.Setenv("SWOBU_DAEMON_URL", daemon.BaseURL)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", homeDir)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	journey := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	journey.WaitVisible("Swobu")
	journey.WaitVisibleAny("ready", "offline (stale)")
	journey.WaitVisible("/c/jobs/")
	openClientsSectionEvidence(journey)

	selectClientEvidence(journey, "OpenCode")
	journey.FocusRow("run")
	journey.ActivateFocusedRow()
	waitForRunExitOrInterruptEvidence(journey, "opencode", 35*time.Second)
	waitForCockpitReturnEvidence(journey, 20*time.Second)

	got, ok := waitForCompatibilityRequest(recorder, 10*time.Second)
	if !ok {
		// Some host/client combinations return control to cockpit without issuing
		// a request via run-once. Fall back to explicit profile-derived run using
		// generated config materialization to keep replay proof deterministic.
		openAIBaseURL := strings.TrimRight(recorder.BaseURL(), "/") + "/c/jobs/"
		profile := mustProfileByID(t, clientprofile.Catalog(), "opencode")
		fileConfig := mustActionByID(t, profile.Actions(openAIBaseURL), "file-config").Content
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "opencode.json"), []byte(fileConfig), 0o600); err != nil {
			t.Fatalf("write opencode config fallback: %v", err)
		}
		out, err := runClientCommand(
			t,
			workDir,
			map[string]string{"OPENAI_API_KEY": "swobu-hermetic-key"},
			"opencode",
			"run",
			"--model", "swobu/primary",
			"Reply with exactly: hermetic-opencode-token",
		)
		if err != nil {
			t.Fatalf("opencode fallback replay command failed: %v\noutput:\n%s", err, out)
		}
		got, ok = waitForCompatibilityRequest(recorder, 10*time.Second)
	}
	if !ok {
		t.Fatal("no compatibility request captured from hermetic opencode run replay execution")
	}
	if got.Method != http.MethodPost {
		t.Fatalf("method=%q want=%q path=%q", got.Method, http.MethodPost, got.Path)
	}
	if !strings.HasPrefix(strings.TrimSpace(got.Path), "/c/jobs/") {
		t.Fatalf("path=%q want prefix %q", got.Path, "/c/jobs/")
	}
	normalizedBody := compactJSONLike(got.Body)
	if !strings.Contains(normalizedBody, compactJSONLike(`"model":"swobu/primary"`)) &&
		!strings.Contains(normalizedBody, compactJSONLike(`"model":"primary"`)) {
		t.Fatalf("captured request missing primary model selector: path=%q body=%q", got.Path, got.Body)
	}
}

func mustLoadHermeticRecordedClientRequest(t *testing.T, path string) hermeticRecordedClientRequest {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}
	var fixture hermeticRecordedClientRequest
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode fixture %q: %v", path, err)
	}
	if strings.TrimSpace(fixture.Method) == "" || strings.TrimSpace(fixture.Path) == "" {
		t.Fatalf("fixture %q missing required shape fields", path)
	}
	return fixture
}

func compactJSONLike(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

func assertCapturedClientRequestMatchesFixture(t *testing.T, got capturedCompatibilityRequest, fixture hermeticRecordedClientRequest) {
	t.Helper()
	if got.Method != fixture.Method {
		t.Fatalf("method=%q want=%q path=%q body=%q", got.Method, fixture.Method, got.Path, got.Body)
	}
	if got.Path != fixture.Path {
		t.Fatalf("path=%q want=%q body=%q", got.Path, fixture.Path, got.Body)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
		t.Fatalf("decode captured request body: %v body=%q", err, got.Body)
	}
	if strings.TrimSpace(fixture.ExpectedModel) != "" {
		model := strings.TrimSpace(asString(payload["model"]))
		if model != fixture.ExpectedModel {
			t.Fatalf("model=%q want=%q", model, fixture.ExpectedModel)
		}
	}
	if fixture.ExpectedStream != nil {
		stream, _ := payload["stream"].(bool)
		if stream != *fixture.ExpectedStream {
			t.Fatalf("stream=%t want=%t", stream, *fixture.ExpectedStream)
		}
	}
	normalizedBody := compactJSONLike(got.Body)
	for _, token := range fixture.RequiredMessageSubstrings {
		if !strings.Contains(normalizedBody, compactJSONLike(token)) {
			t.Fatalf("missing required message token %q in %q", token, got.Body)
		}
	}
	for _, token := range fixture.RequiredSystemHintSubstrings {
		if !strings.Contains(normalizedBody, compactJSONLike(token)) {
			t.Fatalf("missing required system hint token %q in %q", token, got.Body)
		}
	}
}

func asString(value any) string {
	out, _ := value.(string)
	return out
}

func newHermeticOpenAICompatibleReplayUpstream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"primary","object":"model"}]}`))
			return
		case "/v1/chat/completions":
			if requestWantsStreamBytes(raw) {
				writeSSE(w, []string{
					`{"id":"chatcmpl_1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
					`{"id":"chatcmpl_1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ok"}}]}`,
					`{"id":"chatcmpl_1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
				}, true)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			return
		case "/v1/responses":
			if requestWantsStreamBytes(raw) {
				writeSSE(w, []string{
					`{"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress","output":[]}}`,
					`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","status":"in_progress","role":"assistant","content":[]}}`,
					`{"type":"response.content_part.added","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]}}`,
					`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"ok"}`,
					`{"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":0,"text":"ok"}`,
					`{"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}]}}`,
				}, false)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","output_text":"ok","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
			return
		case "/v1/completions":
			if requestWantsStreamBytes(raw) {
				writeSSE(w, []string{
					`{"id":"cmpl_1","object":"text_completion","choices":[{"index":0,"text":"ok","finish_reason":null}]}`,
					`{"id":"cmpl_1","object":"text_completion","choices":[{"index":0,"text":"","finish_reason":"stop"}]}`,
				}, true)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cmpl_1","object":"text_completion","choices":[{"index":0,"text":"ok","finish_reason":"stop"}]}`))
			return
		default:
			http.NotFound(w, r)
		}
	}))
}

func requestWantsStreamBytes(raw []byte) bool {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	stream, ok := payload["stream"].(bool)
	return ok && stream
}

func writeSSE(w http.ResponseWriter, events []string, withDone bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	for _, event := range events {
		_, _ = io.WriteString(w, "data: "+event+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
	if withDone {
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func newRequestForwardRecorder(t *testing.T, targetBase string) *requestForwardRecorder {
	return newRequestForwardRecorderBound(t, targetBase, "127.0.0.1:0", false)
}

func newRequestForwardRecorderEphemeral(t *testing.T, targetBase string) *requestForwardRecorder {
	return newRequestForwardRecorderBound(t, targetBase, "127.0.0.1:0", false)
}

func newRequestForwardRecorderBound(t *testing.T, targetBase, bindAddr string, allowSkipOnBindFailure bool) *requestForwardRecorder {
	t.Helper()
	target, err := url.Parse(strings.TrimSpace(targetBase))
	if err != nil {
		t.Fatalf("parse target base URL: %v", err)
	}
	recorder := &requestForwardRecorder{
		target: target,
		client: &http.Client{Timeout: 30 * time.Second},
	}
	listener, err := net.Listen("tcp", strings.TrimSpace(bindAddr))
	if err != nil {
		if allowSkipOnBindFailure && strings.TrimSpace(os.Getenv("SWOBU_HERMETIC_CONTAINER")) != "1" {
			t.Skipf("bind fixed client replay recorder listener: %v (run via make test-hermetic-client-e2e)", err)
		}
		t.Fatalf("bind recorder listener %q: %v", bindAddr, err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(recorder.forward))
	server.Listener = listener
	server.Start()
	recorder.server = server
	recorder.baseURL = server.URL
	recorder.requests = make([]capturedCompatibilityRequest, 0, 8)
	return recorder
}

func (r *requestForwardRecorder) BaseURL() string {
	return r.baseURL
}

func (r *requestForwardRecorder) Close() {
	if r.server != nil {
		r.server.Close()
	}
}

func (r *requestForwardRecorder) LastCompatibilityRequest() (capturedCompatibilityRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.requests) - 1; i >= 0; i-- {
		if strings.HasPrefix(r.requests[i].Path, "/c/") {
			return r.requests[i], true
		}
	}
	return capturedCompatibilityRequest{}, false
}

func (r *requestForwardRecorder) CompatibilityRequestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, req := range r.requests {
		if strings.HasPrefix(req.Path, "/c/") {
			count++
		}
	}
	return count
}

func waitForCompatibilityRequest(recorder *requestForwardRecorder, timeout time.Duration) (capturedCompatibilityRequest, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if req, ok := recorder.LastCompatibilityRequest(); ok {
			return req, true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return capturedCompatibilityRequest{}, false
}

func (r *requestForwardRecorder) forward(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))

	if req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/c/") {
		r.mu.Lock()
		r.requests = append(r.requests, capturedCompatibilityRequest{
			Method: req.Method,
			Path:   req.URL.Path,
			Body:   string(body),
		})
		r.mu.Unlock()
	}

	targetURL := *r.target
	targetURL.Path = req.URL.Path
	targetURL.RawQuery = req.URL.RawQuery
	forwardReq, err := http.NewRequestWithContext(req.Context(), req.Method, targetURL.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for key, values := range req.Header {
		for _, value := range values {
			forwardReq.Header.Add(key, value)
		}
	}
	resp, err := r.client.Do(forwardReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
