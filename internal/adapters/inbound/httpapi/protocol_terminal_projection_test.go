package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestMessagesClientReceivesImmediateStructuredResponsesError(t *testing.T) {
	setDefaultLogger, logs := testDebugLogger()
	defer setDefaultLogger()
	body := "event: error\ndata: {\"type\":\"error\",\"status_code\":429,\"error\":{\"type\":\"usage_limit_reached\",\"message\":\"The usage limit has been reached\"},\"request_id\":\"req_provider\"}\n\n"
	handler := terminalProjectionHandler(t, terminalProjectionTransport{body: body})
	request := httptest.NewRequest(http.MethodPost, "/c/personal/messages", strings.NewReader(`{"model":"default","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("X-Request-Id", "req_cross_family_error")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Type != "error" || envelope.Error.Type != "rate_limit_error" || envelope.Error.Message != "The usage limit has been reached" || envelope.RequestID != "req_provider" {
		t.Fatalf("Messages error envelope = %#v", envelope)
	}
	for _, want := range []string{
		"failure_class=unavailable", "status_code=429", "backend_error_type=usage_limit_reached",
		"event=provider_error_detail",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs missing %q:\n%s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), "The usage limit has been reached") {
		t.Fatalf("logs exposed provider-controlled message:\n%s", logs.String())
	}
}

type terminalProjectionWorkspaceLookup struct{ workspace routing.Workspace }

func (l terminalProjectionWorkspaceLookup) GetWorkspace(context.Context, routing.WorkspaceSlug) (routing.Workspace, error) {
	return l.workspace, nil
}

type terminalProjectionTransport struct {
	body         string
	documentBody string
	onSend       func(provider.TargetSnapshot)
}

func (t terminalProjectionTransport) Send(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
	if t.onSend != nil {
		t.onSend(target)
	}
	if t.documentBody != "" {
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(t.documentBody), carrier.Meta{})}, nil
	}
	body := t.body
	if body == "" {
		body = "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_provider\",\"model\":\"responses-model\",\"status\":\"in_progress\"}}\n\n" +
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hello from Responses\"}]}}\n\n" +
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_provider\",\"model\":\"responses-model\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hello from Responses\"}]}]}}\n\n"
	}
	return provider.StreamIngress{Stream: carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(body))}}, nil
}

type terminalProjectionRuntime struct {
	codecresolver.RuntimeCodecResolver
	transport terminalProjectionTransport
}

func (r terminalProjectionRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return provider.Backend{
		Target:    target,
		Codec:     protocolcodec.Codec{Protocol: target.ProtocolKind},
		Transport: provider.BindTransport(target, r.transport.Send),
	}, nil
}

func TestChatStreamingClientProjectsResponsesCompletedAsStop(t *testing.T) {
	handler := terminalProjectionHandler(t, terminalProjectionTransport{})
	request := httptest.NewRequest(http.MethodPost, "/c/personal/chat/completions", strings.NewReader(`{"model":"default","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-Id", "req_terminal_projection")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nbody: %s", response.Code, response.Body.String())
	}
	raw := response.Body.Bytes()
	if !bytes.Contains(raw, []byte("Hello from Responses")) {
		t.Fatalf("Chat stream lost assistant text: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"finish_reason":"stop"`)) {
		t.Fatalf("Chat stream terminal = %s, want stop", raw)
	}
	if bytes.Contains(raw, []byte(`"finish_reason":"completed"`)) {
		t.Fatalf("Responses terminal leaked into Chat stream: %s", raw)
	}
	if !bytes.Contains(raw, []byte("data: [DONE]")) {
		t.Fatalf("Chat stream lacks DONE frame: %s", raw)
	}
}

func TestChatStreamingClientProjectsPostIdentityResponsesFailureInStream(t *testing.T) {
	body := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_provider\",\"model\":\"responses-model\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_provider\",\"model\":\"responses-model\",\"status\":\"failed\",\"error\":{\"code\":\"backend_failed\",\"message\":\"provider failed\"}}}\n\n"
	handler := terminalProjectionHandler(t, terminalProjectionTransport{body: body})
	request := httptest.NewRequest(http.MethodPost, "/c/personal/chat/completions", strings.NewReader(`{"model":"default","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-Id", "req_terminal_failure")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	raw := response.Body.Bytes()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want committed stream 200: %s", response.Code, raw)
	}
	if bytes.Contains(raw, []byte(`"finish_reason":"stop"`)) {
		t.Fatalf("failed Responses stream became Chat success: %s", raw)
	}
	if !bytes.Contains(raw, []byte("backend_failed")) || !bytes.Contains(raw, []byte("provider failed")) {
		t.Fatalf("failed Responses stream discarded structured provider error: status=%d body=%s", response.Code, raw)
	}
}

func terminalProjectionHandler(t *testing.T, transport terminalProjectionTransport) Handler {
	t.Helper()
	workspace := terminalProjectionWorkspace(t)
	runtime := terminalProjectionRuntime{RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(), transport: transport}
	ingress := exchange.NewIngress(
		terminalProjectionWorkspaceLookup{workspace: workspace},
		runtime,
		exchange.RuntimePoliciesSpec{PolicyResolver: exchange.StaticWorkspacePolicyResolver{Policy: exchange.DefaultWorkspacePolicy()}},
	)
	return NewHandler(ingress, nil)
}

func terminalProjectionWorkspace(t *testing.T) routing.Workspace {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug("personal")
	routeName, err := routing.ParseRouteName("terminal-route")
	if err != nil {
		t.Fatal(err)
	}
	targetID, _ := routing.ParseTargetID("responses-target")
	model, _ := routing.ParseUpstreamModel("responses-model")
	providerName, _ := routing.ParseProvider("custom", func(raw string) bool { return raw == "custom" })
	connection, _ := routing.NewCustomConnection(providerName, "https://example.test/v1", nil)
	protocol, err := routing.ParseProtocol("responses_stream", providerName, func(routing.Provider, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	tier, _ := routing.NewTier([]routing.Target{target})
	route, _ := routing.NewRoute(routeName, []routing.Tier{tier})
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}
