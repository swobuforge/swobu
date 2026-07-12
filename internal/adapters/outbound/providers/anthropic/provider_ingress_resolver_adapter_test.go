package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
)

type staticCredentialProvider struct {
	token string
}

func (p staticCredentialProvider) ResolveCredential(context.Context, string, string) (string, error) {
	return p.token, nil
}

type recordingEffectSink struct {
	effects []effect.Effect
}

func (s *recordingEffectSink) Commit(_ context.Context, _ string, effects []effect.Effect) error {
	s.effects = append(s.effects, effects...)
	return nil
}

func mustJSONBodyMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return body
}

func TestResolveProviderIngress_UsesContractDeliveryForStreamingRequests(t *testing.T) {
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
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := NewExecutor(server.Client(), staticCredentialProvider{token: "test-token"})
	req := ports.NewProviderRequest(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "claude-sonnet-4-20250514",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "ping"),
			},
			Tools: []canonical.ToolDecl{
				canonical.NewFunctionToolDecl("tool_0", "Read", "read files", canonical.NewToolSchemaObject(`{"type":"object","properties":{"path":{"type":"string"}}}`)),
			},
		}),
		carrier.NewWireDocument("", "", "application/json", nil, []byte(`{}`), carrier.Meta{}),
		exchange.NewExecutionContractForDeliveries(delivery.StreamingDelivery(delivery.FramingSSE), delivery.StreamingDelivery(delivery.FramingSSE)),
		exchange.NewRoutableTarget(
			"backend-a",
			"anthropic",
			server.URL,
			"env:ANTHROPIC_API_KEY",
			protocolkind.Messages,
			"credential_ref",
			"",
			"messages",
		),
	)

	ingress, err := adapter.ResolveProviderIngress(context.Background(), req)
	if err != nil {
		t.Fatalf("ResolveProviderIngress returned error: %v", err)
	}

	stream, ok := ingress.(carrier.WireStream)
	if !ok {
		t.Fatalf("ResolveProviderIngress returned %T, want carrier.WireStream", ingress)
	}
	defer func() { _ = stream.Frames.Close() }()

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

func TestResolveProviderIngress_DoesNotEmitCacheBreakpoints(t *testing.T) {
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

	tool := canonical.NewFunctionToolDecl("Read", "Read", "read files", canonical.NewToolSchemaObject(`{"type":"object","properties":{"path":{"type":"string"}}}`))
	projectedToolName, err := canonical.ProjectedToolName(tool)
	if err != nil {
		t.Fatalf("ProjectedToolName: %v", err)
	}

	adapter := NewExecutor(server.Client(), staticCredentialProvider{token: "test-token"})
	sink := &recordingEffectSink{}
	req := ports.NewProviderRequest(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "claude-sonnet-4-20250514",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "ping"),
			},
			Tools: []canonical.ToolDecl{
				tool,
			},
		}),
		carrier.NewWireDocument("", "", "application/json", nil, []byte(`{}`), carrier.Meta{}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget(
			"backend-a",
			"anthropic",
			server.URL,
			"env:ANTHROPIC_API_KEY",
			protocolkind.Messages,
			"credential_ref",
			"",
			"messages",
		),
		sink,
	)
	req.ExchangeID = "ex-anthropic-cache-breakpoint"

	ingress, err := adapter.ResolveProviderIngress(context.Background(), req)
	if err != nil {
		t.Fatalf("ResolveProviderIngress returned error: %v", err)
	}
	if _, ok := ingress.(carrier.WireDocument); !ok {
		t.Fatalf("ResolveProviderIngress returned %T, want carrier.WireDocument", ingress)
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

	if len(sink.effects) != 0 {
		t.Fatalf("captured effects len=%d want=0", len(sink.effects))
	}
}
