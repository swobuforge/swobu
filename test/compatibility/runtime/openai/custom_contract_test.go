package openai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	customadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/ports"
)

func TestCustomAdapter_OpenAICompatibleContractFamilies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		request      compatibility.CanonicalRequest
		deliveryMode compatibility.DeliveryMode
		wantPath     string
		wantMethod   string
		protocolKind protocolsurface.Kind
	}{
		{
			name:         "chat_completions",
			request:      compatibility.NewDialogRequest("m", []compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")}),
			deliveryMode: compatibility.DeliveryModeBuffered,
			wantPath:     "/v1/chat/completions",
			wantMethod:   http.MethodPost,
			protocolKind: "chat_completions",
		},
		{
			name:         "responses",
			request:      compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{Model: "m", InputText: "hi"}),
			deliveryMode: compatibility.DeliveryModeBuffered,
			wantPath:     "/v1/responses",
			wantMethod:   http.MethodPost,
			protocolKind: "responses",
		},
		{
			name:         "completions",
			request:      compatibility.NewPromptRequest("m", "hi"),
			deliveryMode: compatibility.DeliveryModeBuffered,
			wantPath:     "/v1/completions",
			wantMethod:   http.MethodPost,
			protocolKind: "completions",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			var gotMethod string

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotMethod = r.Method
				w.Header().Set("Content-Type", "application/json")
				switch tc.wantPath {
				case "/v1/chat/completions":
					_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
				case "/v1/responses":
					_, _ = w.Write([]byte(`{"id":"resp_1","model":"m","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"output_text":"ok"}`))
				case "/v1/completions":
					_, _ = w.Write([]byte(`{"id":"cmpl_1","model":"m","choices":[{"text":"ok","finish_reason":"stop"}]}`))
				default:
					_, _ = w.Write([]byte(`{}`))
				}
			}))
			defer upstream.Close()

			executor := customadapter.NewExecutor(upstream.Client(), staticConformanceResolver("token-123"))
			resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
				tc.request,
				ports.NewExecutionContract(tc.deliveryMode),
				ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "cred-1", tc.protocolKind, "", ""),
			))
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if got := resp.DeliveryMode(); got != compatibility.DeliveryModeBuffered {
				t.Fatalf("delivery mode = %q, want %q", got, compatibility.DeliveryModeBuffered)
			}
			if resp.Output() == nil {
				t.Fatal("output = nil, want canonical output")
			}

			if gotMethod != tc.wantMethod {
				t.Fatalf("method = %q, want %q", gotMethod, tc.wantMethod)
			}
			if gotPath != tc.wantPath {
				t.Fatalf("path = %q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}

// Covers conformance gate (docs section 6): streaming first-byte behavior for
// the declared OpenAI-compatible support band.
func TestCustomAdapter_OpenAICompatibleStreamingFirstByte(t *testing.T) {
	t.Parallel()

	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"chatcmpl_1","model":"m","choices":[{"delta":{"content":"o"},"finish_reason":""}]}`,
			"",
			`data: {"id":"chatcmpl_1","model":"m","choices":[{"delta":{"content":"k"},"finish_reason":"stop"}]}`,
			"",
			"data: [DONE]",
			"",
		}, "\n")))
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), staticConformanceResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("m", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		}), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "cred-1", protocolsurface.ChatCompletions, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/chat/completions")
	}
	if got := resp.DeliveryMode(); got != compatibility.DeliveryModeStreaming {
		t.Fatalf("delivery mode = %q, want %q", got, compatibility.DeliveryModeStreaming)
	}

	first, err := resp.Stream().Next()
	if err != nil {
		t.Fatalf("stream first event returned error: %v", err)
	}
	if first.Kind != compatibility.OutputEventStarted {
		t.Fatalf("first event kind = %q, want %q", first.Kind, compatibility.OutputEventStarted)
	}
}

func TestCustomAdapter_OpenAICompatibleStreamingFirstByte_Responses(t *testing.T) {
	t.Parallel()

	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_1","model":"m"}}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n")))
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), staticConformanceResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{Model: "m", InputText: "hi"}), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "cred-1", protocolsurface.Responses, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/responses")
	}
	first, err := resp.Stream().Next()
	if err != nil {
		t.Fatalf("stream first event returned error: %v", err)
	}
	if first.Kind != compatibility.OutputEventStarted {
		t.Fatalf("first event kind = %q, want %q", first.Kind, compatibility.OutputEventStarted)
	}
}

func TestCustomAdapter_OpenAICompatibleStreamingFirstByte_Completions(t *testing.T) {
	t.Parallel()

	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"id":"cmpl_1","model":"m","choices":[{"text":"o","finish_reason":""}]}`,
			"",
			`data: {"id":"cmpl_1","model":"m","choices":[{"text":"","finish_reason":"stop"}]}`,
			"",
			"data: [DONE]",
			"",
		}, "\n")))
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), staticConformanceResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewPromptRequest("m", "hi"), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "cred-1", protocolsurface.Completions, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotPath != "/v1/completions" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/completions")
	}
	first, err := resp.Stream().Next()
	if err != nil {
		t.Fatalf("stream first event returned error: %v", err)
	}
	if first.Kind != compatibility.OutputEventStarted {
		t.Fatalf("first event kind = %q, want %q", first.Kind, compatibility.OutputEventStarted)
	}
}

// Covers conformance gate fail-fast truth for unsupported protocol kinds.
func TestCustomAdapter_OpenAICompatibleFailFastUnsupportedProtocolKind(t *testing.T) {
	t.Parallel()

	executor := customadapter.NewExecutor(http.DefaultClient, staticConformanceResolver("token-123"))
	_, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("m", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		}), ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "custom", "https://example.test/v1", "cred-1", "unsupported_protocol", "", ""),
	))
	if err == nil {
		t.Fatal("Execute returned nil error, want fail-fast error")
	}
	var swobuErr compatibility.Error
	if !errors.As(err, &swobuErr) {
		t.Fatalf("error type = %T, want compatibility.Error", err)
	}
	if swobuErr.Code != compatibility.ErrorCodeBadEndpoint {
		t.Fatalf("error code = %q, want %q", swobuErr.Code, compatibility.ErrorCodeBadEndpoint)
	}
}

func TestCustomAdapter_OpenAICompatibleStreamingTerminalUsageReplay(t *testing.T) {
	t.Parallel()

	fixture := mustReadRuntimeFixture(t, "openai_responses_stream_usage.sse")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), staticConformanceResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{Model: "m", InputText: "hi"}),
		ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "cred-1", protocolsurface.Responses, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	completed, doneErr := readCompletedEvent(resp.Stream())
	if doneErr != nil {
		t.Fatalf("read completed event: %v", doneErr)
	}
	assertUsage(t, completed.Usage, 10, 2, 0)
}

func TestCustomAdapter_OpenAICompatibleBufferedUsageReplay(t *testing.T) {
	t.Parallel()

	fixture := mustReadRuntimeFixture(t, "openai_responses_buffered_usage.json")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), staticConformanceResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{Model: "m", InputText: "hi"}),
		ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "cred-1", protocolsurface.Responses, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got := resp.Output(); got == nil {
		t.Fatal("output = nil, want canonical output")
	} else {
		assertUsage(t, got.Usage(), 10, 2, 0)
	}
}

func mustReadRuntimeFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := "testdata/" + name
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}

func readCompletedEvent(stream compatibility.CanonicalOutputEventStream) (compatibility.OutputEvent, error) {
	for {
		event, err := stream.Next()
		if err != nil {
			return compatibility.OutputEvent{}, err
		}
		if event.Kind == compatibility.OutputEventCompleted {
			return event, nil
		}
	}
}

func assertUsage(t *testing.T, usage compatibility.TokenUsage, wantInput int, wantOutput int, wantCacheRead int) {
	t.Helper()

	gotInput, ok := usage.InputTokens()
	if !ok || gotInput != wantInput {
		t.Fatalf("input tokens = (%d,%v), want (%d,true)", gotInput, ok, wantInput)
	}
	gotOutput, ok := usage.OutputTokens()
	if !ok || gotOutput != wantOutput {
		t.Fatalf("output tokens = (%d,%v), want (%d,true)", gotOutput, ok, wantOutput)
	}
	gotCacheRead, ok := usage.CacheReadTokens()
	if !ok || gotCacheRead != wantCacheRead {
		t.Fatalf("cache read tokens = (%d,%v), want (%d,true)", gotCacheRead, ok, wantCacheRead)
	}
	if gotCacheWrite, ok := usage.CacheWriteTokens(); ok {
		t.Fatalf("cache write tokens = (%d,%v), want absent", gotCacheWrite, ok)
	}
}

type staticConformanceResolver string

func (r staticConformanceResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return string(r), nil
}
