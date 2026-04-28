package providers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	anthropicadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/anthropic"
	customadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/devtools/livematrix"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/ports"
)

func TestLiveMatrixRecordedCapturesDecodeOffline(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "fixtures", "live_matrix", "records", "*.json"))
	if err != nil {
		t.Fatalf("glob captures: %v", err)
	}
	if len(paths) == 0 {
		t.Skip("no recorded live matrix captures; run: go run ./internal/devtools/cmd/livematrix")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read capture: %v", readErr)
			}
			var capture livematrix.Capture
			if err := json.Unmarshal(raw, &capture); err != nil {
				t.Fatalf("decode capture: %v", err)
			}
			if !captureHasProviderEdgePayload(capture) {
				t.Skipf("capture has no provider-edge payload (likely fail-fast before provider call): %s", filepath.Base(path))
			}
			if capture.Response.StatusCode == 0 || capture.Response.Body == "" {
				t.Fatalf("capture missing response payload: %+v", capture.Response)
			}
			if err := decodeCaptureOffline(capture); err != nil {
				t.Fatalf("offline decode failed: %v", err)
			}
		})
	}
}

func captureHasProviderEdgePayload(capture livematrix.Capture) bool {
	if capture.Request.StatusCode != 0 || capture.Request.Body != "" || capture.Response.StatusCode != 0 || capture.Response.Body != "" {
		return true
	}
	if capture.Session == nil {
		return false
	}
	for _, event := range capture.Session.Events {
		if event.Direction == livematrix.DirectionSwobuToProvider || event.Direction == livematrix.DirectionProviderToSwobu {
			return true
		}
	}
	return false
}

func TestLiveMatrixOfflineDecode_FailsOnTranscriptDrift(t *testing.T) {
	capture := livematrix.Capture{
		ScenarioCase: livematrix.ScenarioCase{
			ID:        "drift",
			Provider:  "custom",
			Protocol:  "chat_completions",
			Transport: "http_post",
			Model:     "m",
		},
		Error: "",
		Session: &livematrix.SessionTraceCapture{
			TraceID:   "s-1",
			RequestID: "r-1",
			Events: []livematrix.TraceEvent{
				{
					Seq:          1,
					Direction:    livematrix.DirectionSwobuToProvider,
					AttemptIndex: 1,
					Wire: livematrix.CapturedWire{
						Method: "POST",
						Path:   "/chat/completions",
						Body:   `{"model":"m","messages":[{"role":"user","content":"DIFF"}]}`,
					},
				},
				{
					Seq:          2,
					Direction:    livematrix.DirectionProviderToSwobu,
					AttemptIndex: 1,
					Wire: livematrix.CapturedWire{
						StatusCode: 200,
						Headers:    map[string]string{"Content-Type": "application/json"},
						Body:       `{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
					},
				},
			},
		},
		Request: livematrix.CapturedWire{
			Method: "POST",
			Path:   "/chat/completions",
			Body:   `{"model":"m","messages":[{"role":"user","content":"DIFF"}]}`,
		},
		Response: livematrix.CapturedWire{
			StatusCode: 200,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
		},
	}
	err := decodeCaptureOffline(capture)
	if err == nil || !strings.Contains(err.Error(), "request drift") {
		t.Fatalf("expected request drift error, got %v", err)
	}
}

type offlineExchange struct {
	request  livematrix.CapturedWire
	response livematrix.CapturedWire
}

type offlineRequestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type offlineRequestShape struct {
	Model    string                  `json:"model"`
	Prompt   string                  `json:"prompt"`
	Input    any                     `json:"input"`
	Messages []offlineRequestMessage `json:"messages"`
}

func decodeCaptureOffline(capture livematrix.Capture) error {
	protocol := protocolFromScenarioCaseNoFatal(capture.ScenarioCase.Protocol)
	if protocol == "" {
		return fmt.Errorf("unsupported protocol %q", capture.ScenarioCase.Protocol)
	}
	exchanges := exchangesFromCapture(capture)
	if len(exchanges) == 0 {
		return fmt.Errorf("capture has no offline exchanges")
	}
	normalizeStructuredResponses(protocol, capture, exchanges)

	var (
		mu       sync.Mutex
		callIdx  int
		mismatch string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if callIdx >= len(exchanges) {
			mismatch = fmt.Sprintf("unexpected extra call %d path=%s", callIdx+1, r.URL.Path)
			http.Error(w, mismatch, http.StatusInternalServerError)
			return
		}
		expected := exchanges[callIdx]
		callIdx++
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if expected.request.Method != "" && !strings.EqualFold(expected.request.Method, r.Method) {
			mismatch = fmt.Sprintf("request drift method got=%s want=%s", r.Method, expected.request.Method)
		}
		if expected.request.Path != "" && expected.request.Path != r.URL.Path {
			mismatch = fmt.Sprintf("request drift path got=%s want=%s", r.URL.Path, expected.request.Path)
		}
		if expected.request.Body != "" && expected.request.Body != string(body) {
			if mismatchBody := requestBodyMismatch(expected.request.Body, string(body)); mismatchBody != "" {
				mismatch = mismatchBody
			}
		}
		if mismatch != "" {
			http.Error(w, mismatch, http.StatusInternalServerError)
			return
		}
		for key, value := range expected.response.Headers {
			w.Header().Set(key, value)
		}
		status := expected.response.StatusCode
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(expected.response.Body))
	}))
	defer upstream.Close()

	provider := strings.TrimSpace(strings.ToLower(capture.ScenarioCase.Provider))
	adapter := offlineAdapter(provider, upstream)
	if adapter == nil {
		return nil
	}
	model := strings.TrimSpace(capture.ScenarioCase.Model)
	if model == "" {
		model = "m"
	}
	req := requestForProtocolNoFatal(protocol, model, capture.ScenarioCase.Scenario)
	if req == nil {
		return fmt.Errorf("unsupported protocol %q", protocol)
	}
	delivery := compatibility.DeliveryModeBuffered
	if strings.EqualFold(capture.ScenarioCase.Transport, "sse_streaming") {
		delivery = compatibility.DeliveryModeStreaming
	}
	resp, execErr := adapter.Execute(context.Background(), ports.NewExecuteRequest(
		req,
		ports.NewExecutionContract(delivery),
		ports.NewRoutableTarget("backend-live-offline", provider, upstream.URL, "cred-1", protocol, "", ""),
	))
	if mismatch != "" {
		return fmt.Errorf(mismatch)
	}
	expectedFailure := capture.Response.StatusCode >= 400 || strings.TrimSpace(capture.Error) != ""
	if expectedFailure {
		if execErr == nil {
			return fmt.Errorf("offline decode expected failure (status=%d error=%q) but got success", capture.Response.StatusCode, capture.Error)
		}
		return nil
	}
	if execErr != nil {
		return fmt.Errorf("offline decode execute failed: %w", execErr)
	}
	if resp.DeliveryMode() != delivery {
		return fmt.Errorf("delivery mode = %q, want %q", resp.DeliveryMode(), delivery)
	}
	if delivery == compatibility.DeliveryModeBuffered {
		if out := resp.Output(); out == nil || out.Model() == "" {
			return fmt.Errorf("buffered output missing model: %#v", out)
		}
	}
	_ = resp.Close()
	if callIdx != len(exchanges) {
		return fmt.Errorf("offline call count drift got=%d want=%d", callIdx, len(exchanges))
	}
	return nil
}

func exchangesFromCapture(capture livematrix.Capture) []offlineExchange {
	if capture.Session == nil || len(capture.Session.Events) == 0 {
		return []offlineExchange{{request: capture.Request, response: capture.Response}}
	}
	type pair struct {
		request  livematrix.CapturedWire
		response livematrix.CapturedWire
	}
	byAttempt := map[int]*pair{}
	for _, event := range capture.Session.Events {
		attempt := event.AttemptIndex
		if attempt <= 0 {
			continue
		}
		if event.Direction != livematrix.DirectionSwobuToProvider && event.Direction != livematrix.DirectionProviderToSwobu {
			continue
		}
		entry, ok := byAttempt[attempt]
		if !ok {
			entry = &pair{}
			byAttempt[attempt] = entry
		}
		if event.Direction == livematrix.DirectionSwobuToProvider {
			entry.request = event.Wire
			continue
		}
		entry.response = event.Wire
	}
	if len(byAttempt) == 0 {
		return []offlineExchange{{request: capture.Request, response: capture.Response}}
	}
	attempts := make([]int, 0, len(byAttempt))
	for attempt := range byAttempt {
		attempts = append(attempts, attempt)
	}
	sort.Ints(attempts)
	out := make([]offlineExchange, 0, len(attempts))
	for _, attempt := range attempts {
		entry := byAttempt[attempt]
		out = append(out, offlineExchange{request: entry.request, response: entry.response})
	}
	return out
}

func normalizeStructuredResponses(protocol protocolsurface.Kind, capture livematrix.Capture, exchanges []offlineExchange) {
	if protocol != protocolsurface.Messages {
		return
	}
	if !validJSON(capture.Client.Response.Body) {
		return
	}
	for i := range exchanges {
		if validJSON(exchanges[i].response.Body) {
			continue
		}
		exchanges[i].response.Body = capture.Client.Response.Body
		if exchanges[i].response.StatusCode == 0 {
			exchanges[i].response.StatusCode = capture.Client.Response.StatusCode
		}
		if len(exchanges[i].response.Headers) == 0 {
			exchanges[i].response.Headers = capture.Client.Response.Headers
		}
	}
}

func offlineAdapter(provider string, upstream *httptest.Server) executeAdapter {
	resolver := matrixCredentialResolver{}
	switch provider {
	case "anthropic":
		return anthropicadapter.NewExecutor(upstream.Client(), resolver)
	default:
		return customadapter.NewExecutor(upstream.Client(), resolver)
	}
}

func protocolFromScenarioCaseNoFatal(raw string) protocolsurface.Kind {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "chat_completions":
		return protocolsurface.ChatCompletions
	case "responses":
		return protocolsurface.Responses
	case "completions":
		return protocolsurface.Completions
	case "messages":
		return protocolsurface.Messages
	default:
		return ""
	}
}

func requestForProtocolNoFatal(protocol protocolsurface.Kind, model string, scenario string) compatibility.CanonicalRequest {
	userText := "hi"
	if strings.EqualFold(strings.TrimSpace(scenario), "tool_min") {
		userText = "Call tool noop with x=1 and stop."
	}
	if strings.EqualFold(strings.TrimSpace(scenario), "text_min") {
		userText = "Reply with OK"
	}
	switch protocol {
	case protocolsurface.ChatCompletions, protocolsurface.Messages:
		return compatibility.NewDialogRequest(model, []compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, userText)})
	case protocolsurface.Responses:
		return compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{Model: model, InputText: userText})
	case protocolsurface.Completions:
		return compatibility.NewPromptRequest(model, userText)
	default:
		return nil
	}
}

func requestBodyMismatch(expectedBody string, gotBody string) string {
	var expected offlineRequestShape
	var got offlineRequestShape
	if err := json.Unmarshal([]byte(expectedBody), &expected); err != nil {
		if expectedBody != gotBody {
			return fmt.Sprintf("request drift body got=%s want=%s", gotBody, expectedBody)
		}
		return ""
	}
	if err := json.Unmarshal([]byte(gotBody), &got); err != nil {
		return fmt.Sprintf("request drift body got=%s want=%s", gotBody, expectedBody)
	}
	if strings.TrimSpace(expected.Model) != "" && expected.Model != got.Model {
		return fmt.Sprintf("request drift model got=%q want=%q", got.Model, expected.Model)
	}
	expectedText := requestPrimaryText(expected)
	gotText := requestPrimaryText(got)
	if expectedText != "" && expectedText != gotText {
		return fmt.Sprintf("request drift user_text got=%q want=%q", gotText, expectedText)
	}
	return ""
}

func requestPrimaryText(request offlineRequestShape) string {
	for _, message := range request.Messages {
		if strings.EqualFold(message.Role, "user") && strings.TrimSpace(message.Content) != "" {
			return strings.TrimSpace(message.Content)
		}
	}
	if strings.TrimSpace(request.Prompt) != "" {
		return strings.TrimSpace(request.Prompt)
	}
	switch typed := request.Input.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func validJSON(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	var decoded any
	return json.Unmarshal([]byte(raw), &decoded) == nil
}
