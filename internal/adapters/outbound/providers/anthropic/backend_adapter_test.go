package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/testkit/providertest"
)

type staticCredentialProvider struct {
	token string
}

func (p staticCredentialProvider) ResolveCredential(context.Context, string, string) (string, error) {
	return p.token, nil
}

func mustJSONBodyMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return body
}

func TestSendProviderRequest_UsesContractDeliveryForStreamingRequests(t *testing.T) {
	t.Parallel()

	sawBody := make(chan string, 1)
	sawErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sawErr <- err
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = r.Body.Close()
		sawBody <- string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := NewExecutor(server.Client(), staticCredentialProvider{token: "test-token"})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("claude-sonnet-4-20250514"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "ping"),
		},
		Tools: canonicaltest.SpecifiedToolSet(t, canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "tool_0"), "read files", canonicaltest.Schema(t, `{"type":"object","properties":{"path":{"type":"string"}}}`), canonical.Unspecified[bool]())),
	})
	target := provider.NewTargetSnapshot(
		"backend-a",
		"anthropic",
		server.URL,
		"env:ANTHROPIC_API_KEY",
		protocolkind.Messages,

		"",
		"messages")
	target.Model = request.Model()

	backend, err := adapter.ResolveBackend(target)
	if err != nil {
		t.Fatalf("ResolveBackend returned error: %v", err)
	}
	doc, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	ingress, err := backend.Transport.Send(context.Background(), doc)
	if err != nil {
		t.Fatalf("SendProviderRequest returned error: %v", err)
	}

	streamIngress, ok := ingress.(provider.StreamIngress)
	if !ok {
		t.Fatalf("Send returned %T, want provider.StreamIngress", ingress)
	}
	stream := streamIngress.Stream
	defer func() { _ = stream.Body.Close() }()

	var body string
	select {
	case err := <-sawErr:
		t.Fatalf("read request body: %v", err)
	case body = <-sawBody:
	}

	if !strings.Contains(body, `"stream":true`) {
		t.Fatalf("request body = %s, want stream=true for streaming contract", body)
	}
	if strings.Contains(body, `"stream":false`) {
		t.Fatalf("request body = %s, want streaming request body, not stream=false", body)
	}
	if !strings.Contains(body, `"tools":[`) {
		t.Fatalf("request body = %s, want tools surface preserved", body)
	}
}

func TestSendProviderRequest_DoesNotEmitCacheBreakpoints(t *testing.T) {
	sawBody := make(chan string, 1)
	sawErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sawErr <- err
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = r.Body.Close()
		sawBody <- string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	tool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "Read"), "read files", canonicaltest.Schema(t, `{"type":"object","properties":{"path":{"type":"string"}}}`), canonical.Unspecified[bool]())
	projectedToolName := providertest.ProjectedToolName(t, tool)

	adapter := NewExecutor(server.Client(), staticCredentialProvider{token: "test-token"})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("claude-sonnet-4-20250514"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "ping"),
		},
		Tools: canonicaltest.SpecifiedToolSet(t, tool),
	})
	target := provider.NewTargetSnapshot(
		"backend-a",
		"anthropic",
		server.URL,
		"env:ANTHROPIC_API_KEY",
		protocolkind.Messages,

		"",
		"messages")
	target.Model = request.Model()

	backend, err := adapter.ResolveBackend(target)
	if err != nil {
		t.Fatalf("ResolveBackend returned error: %v", err)
	}
	doc, decisions, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("Encode decisions = %#v, want none for exact tool lowering", decisions)
	}
	ingress, err := backend.Transport.Send(context.Background(), doc)
	if err != nil {
		t.Fatalf("SendProviderRequest returned error: %v", err)
	}
	if _, ok := ingress.(provider.DocumentIngress); !ok {
		t.Fatalf("Send returned %T, want provider.DocumentIngress", ingress)
	}

	var body string
	select {
	case err := <-sawErr:
		t.Fatalf("read request body: %v", err)
	case body = <-sawBody:
	}
	payload := mustJSONBodyMap(t, []byte(body))
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages=%T len=%d want one message", payload["messages"], len(messages))
	}
	firstMsg, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("messages[0]=%T want map[string]any", messages[0])
	}
	content, ok := firstMsg["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("message content=%T len=%d want content array", firstMsg["content"], len(content))
	}
	firstPart, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0]=%T want map[string]any", content[0])
	}
	if _, ok := firstPart["cache_control"]; ok {
		t.Fatalf("content[0].cache_control must be omitted")
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools=%T len=%d want one tool", payload["tools"], len(tools))
	}
	toolBody, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tools[0]=%T want map[string]any", tools[0])
	}
	if got, _ := toolBody["name"].(string); got != projectedToolName {
		t.Fatalf("tool name=%q want %q", got, projectedToolName)
	}
	if _, ok := toolBody["cache_control"]; ok {
		t.Fatalf("tool.cache_control must be omitted")
	}

}
