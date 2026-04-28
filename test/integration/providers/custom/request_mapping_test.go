package custom

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	customadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/ports"
)

func TestCustomAdapter_MapsSupportedCanonicalRequestsToOpenAICompatiblePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		basePath     string
		request      compatibility.CanonicalRequest
		deliveryMode compatibility.DeliveryMode
		wantMethod   string
		wantPath     string
		wantBody     string
		protocolKind protocolsurface.Kind
	}{
		{
			name:     "chat buffered",
			basePath: "/openai/v1/",
			request: compatibility.NewDialogRequest(
				"m",
				[]compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")},
			),
			deliveryMode: compatibility.DeliveryModeBuffered,
			wantMethod:   http.MethodPost,
			wantPath:     "/openai/v1/chat/completions",
			wantBody:     `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
			protocolKind: "chat_completions",
		},
		{
			name:         "responses streaming",
			basePath:     "/v1",
			request:      compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{Model: "m", InputText: "hi"}),
			deliveryMode: compatibility.DeliveryModeStreaming,
			wantMethod:   http.MethodPost,
			wantPath:     "/v1/responses",
			wantBody:     `{"model":"m","input":"hi","stream":true}`,
			protocolKind: "responses",
		},
		{
			name:         "completions buffered",
			basePath:     "/v1",
			request:      compatibility.NewPromptRequest("m", "hi"),
			deliveryMode: compatibility.DeliveryModeBuffered,
			wantMethod:   http.MethodPost,
			wantPath:     "/v1/completions",
			wantBody:     `{"model":"m","prompt":"hi"}`,
			protocolKind: "completions",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod string
			var gotPath string
			var gotAuth string
			var gotCallerUA string
			var gotBody string

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotAuth = strings.TrimSpace(r.Header.Get("Authorization"))
				gotCallerUA = strings.TrimSpace(r.Header.Get("User-Agent"))
				raw, _ := io.ReadAll(r.Body)
				gotBody = string(raw)
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/openai/v1/chat/completions", "/v1/chat/completions":
					_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
				case "/v1/responses":
					_, _ = w.Write([]byte(`{"id":"resp_1","model":"m","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"output_text":"ok"}`))
				case "/v1/completions":
					_, _ = w.Write([]byte(`{"id":"cmpl_1","model":"m","choices":[{"text":"ok","finish_reason":"stop"}]}`))
				}
			}))
			defer upstream.Close()

			executor := customadapter.NewExecutor(upstream.Client(), staticCredentialResolver("token-123"))
			_, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
				tc.request,
				ports.NewExecutionContract(tc.deliveryMode),
				ports.NewRoutableTarget("backend-a", "custom", upstream.URL+tc.basePath, "cred-1", tc.protocolKind, "", ""),
			))
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}

			if gotMethod != tc.wantMethod {
				t.Fatalf("method = %q, want %q", gotMethod, tc.wantMethod)
			}
			if gotPath != tc.wantPath {
				t.Fatalf("path = %q, want %q", gotPath, tc.wantPath)
			}
			if gotAuth != "Bearer token-123" {
				t.Fatalf("auth = %q, want %q", gotAuth, "Bearer token-123")
			}
			if gotCallerUA != "swobu/dev" {
				t.Fatalf("user agent = %q, want %q", gotCallerUA, "swobu/dev")
			}
			if gotBody != tc.wantBody {
				t.Fatalf("body = %q, want %q", gotBody, tc.wantBody)
			}
		})
	}
}

func TestCustomAdapter_MapsResponseStateAndStructuredInput(t *testing.T) {
	t.Parallel()

	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"m","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"output_text":"ok"}`))
	}))
	defer upstream.Close()

	req := compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
		Model:              "m",
		PreviousResponseID: "resp_123",
		PromptCacheKey:     "repo-alpha",
		Items: []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "continue"),
			compatibility.NewToolUseItem(compatibility.ItemAuthorAssistant, "", "call_1", "grep", map[string]any{"pattern": "TODO"}),
			compatibility.NewToolResultItem(compatibility.ItemAuthorTool, "call_1", "2 hits"),
		},
	})

	executor := customadapter.NewExecutor(upstream.Client(), nil)
	_, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		req, ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "", "responses", "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	want := `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},{"type":"function_call","call_id":"call_1","name":"grep","arguments":"{\"pattern\":\"TODO\"}"},{"type":"function_call_output","call_id":"call_1","output":"2 hits"}],"previous_response_id":"resp_123","prompt_cache_key":"repo-alpha"}`
	if gotBody != want {
		t.Fatalf("body = %q, want %q", gotBody, want)
	}
}

func TestCustomAdapter_RealizesResponsesContinuityOntoChatCompletions(t *testing.T) {
	t.Parallel()

	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	request := compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
		Model:              "m",
		PreviousResponseID: "resp_prev",
		Thread: []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
			compatibility.NewTextItem(compatibility.ItemAuthorAssistant, "hello"),
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "continue"),
		},
		LastTurn: []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "continue"),
		},
	})

	executor := customadapter.NewExecutor(upstream.Client(), nil)
	_, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		request, ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	want := `{"model":"m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"},{"role":"user","content":"continue"}]}`
	if gotBody != want {
		t.Fatalf("body = %q, want %q", gotBody, want)
	}
}

func TestCustomAdapter_EncodesStructuredConversationPartsForOpenAICompatibleChat(t *testing.T) {
	t.Parallel()

	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	request := compatibility.NewDialogRequest(
		"m",
		[]compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorAssistant, "Let me calculate that."),
			compatibility.NewToolUseItem(compatibility.ItemAuthorAssistant, "", "toolu_1", "calculator", map[string]any{"expr": "2+2"}),
			compatibility.NewToolResultItem(compatibility.ItemAuthorTool, "toolu_1", "4"),
		},
	)

	executor := customadapter.NewExecutor(upstream.Client(), nil)
	_, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		request, ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "", "chat_completions", "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	want := `{"model":"m","messages":[{"role":"assistant","content":"Let me calculate that.","tool_calls":[{"id":"toolu_1","type":"function","function":{"name":"calculator","arguments":"{\"expr\":\"2+2\"}"}}]},{"role":"tool","content":"4","tool_call_id":"toolu_1"}]}`
	if gotBody != want {
		t.Fatalf("body = %q, want %q", gotBody, want)
	}
}

func TestCustomAdapter_DecodesGzipBackendResponsesBeforeReturning(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		_ = gz.Close()
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), nil)
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("m", []compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")}), ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "", "chat_completions", "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	typed, ok := resp.Output().(compatibility.CanonicalOutputValue)
	if !ok {
		t.Fatalf("output type = %T, want compatibility.CanonicalOutputValue", resp.Output())
	}
	if got := typed.Text(); got != "ok" {
		t.Fatalf("output text = %q, want %q", got, "ok")
	}
}

type staticCredentialResolver string

func (r staticCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return string(r), nil
}
