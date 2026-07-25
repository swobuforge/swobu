package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

const imageIncidentPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2ZAAAAABJRU5ErkJggg=="

type imageIncidentWorkspaceLookup struct{ workspace routing.Workspace }

func (l imageIncidentWorkspaceLookup) GetWorkspace(context.Context, routing.WorkspaceSlug) (routing.Workspace, error) {
	return l.workspace, nil
}

type imageIncidentTransport struct {
	calls     int
	documents []carrier.Document
}

func (t *imageIncidentTransport) Send(_ context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
	t.calls++
	t.documents = append(t.documents, document.Clone())
	return provider.DocumentIngress{Document: carrier.NewDocument(
		target.ProtocolKind,
		"application/json",
		nil,
		[]byte(`{
			"id":"chatcmpl_image",
			"object":"chat.completion",
			"created":1,
			"model":"chat-model",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`),
		carrier.Meta{},
	)}, nil
}

type imageIncidentRuntime struct {
	codecresolver.RuntimeCodecResolver
	transport *imageIncidentTransport
}

func (r imageIncidentRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return provider.Backend{
		Target:    target,
		Codec:     protocolcodec.Codec{Protocol: target.ProtocolKind},
		Transport: provider.BindTransport(target, r.transport.Send),
	}, nil
}

func TestResponsesImageIncidentStrictChatRouteReturnsTyped502WithoutProviderCall(t *testing.T) {
	restore, logs := testDebugLogger()
	defer restore()
	transport := &imageIncidentTransport{}
	handler := imageIncidentHandler(t, compat.CompatibilityStrict, transport)
	request := httptest.NewRequest(http.MethodPost, "/c/personal/responses", strings.NewReader(imageIncidentResponsesRequest()))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-Id", "req_image_strict")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502\nbody: %s", response.Code, response.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != string(canonical.ErrorCodeNoCompatibleTarget) {
		t.Fatalf("error code = %q, want %q", body.Error.Code, canonical.ErrorCodeNoCompatibleTarget)
	}
	if transport.calls != 0 {
		t.Fatalf("provider transport calls = %d, want zero", transport.calls)
	}
	for _, want := range []string{
		"event=responses_request_images",
		"decode_view=full",
		"decode_view=rebased_current",
		"event=request_outcome",
		"status_code=502",
		"target_id=chat-image",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, logs.String())
		}
	}
}

func TestResponsesImageIncidentCompatibilityCallsChatWithSyntheticImageOnce(t *testing.T) {
	transport := &imageIncidentTransport{}
	handler := imageIncidentHandler(t, compat.CompatibilityCompat, transport)
	request := httptest.NewRequest(http.MethodPost, "/c/personal/responses", strings.NewReader(imageIncidentResponsesRequest()))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-Id", "req_image_compat")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nbody: %s", response.Code, response.Body.String())
	}
	if transport.calls != 1 || len(transport.documents) != 1 {
		t.Fatalf("provider transport calls = %d documents=%d, want one", transport.calls, len(transport.documents))
	}
	var payload struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(transport.documents[0].RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	roles := make([]string, len(payload.Messages))
	syntheticCount := 0
	imageCount := 0
	for index, message := range payload.Messages {
		roles[index] = message.Role
		if message.Role != "user" || len(message.Content) == 0 || message.Content[0] != '[' {
			continue
		}
		var content []map[string]any
		if err := json.Unmarshal(message.Content, &content); err != nil {
			t.Fatal(err)
		}
		if len(content) > 0 {
			if marker, _ := content[0]["text"].(string); strings.Contains(marker, `"kind":"tool_result_image"`) {
				syntheticCount++
			}
		}
		for _, part := range content {
			if part["type"] == "image_url" {
				imageCount++
			}
		}
	}
	if got := strings.Join(roles, ","); !strings.Contains(got, "assistant,tool,user,assistant,user") {
		t.Fatalf("provider message roles = %s", got)
	}
	if syntheticCount != 1 || imageCount != 1 {
		t.Fatalf("synthetic messages=%d images=%d payload=%s", syntheticCount, imageCount, transport.documents[0].RawBytes())
	}
	if !bytes.Contains(transport.documents[0].RawBytes(), []byte("data:image/png;base64,"+imageIncidentPNG)) {
		t.Fatalf("provider request lost image: %s", transport.documents[0].RawBytes())
	}
	if bytes.Contains(transport.documents[0].RawBytes(), []byte("all_turns")) {
		t.Fatalf("Chat provider request leaked Responses reasoning context: %s", transport.documents[0].RawBytes())
	}
}

func imageIncidentHandler(t *testing.T, mode compat.CompatibilityMode, transport *imageIncidentTransport) Handler {
	t.Helper()
	workspace := imageIncidentWorkspace(t)
	runtime := imageIncidentRuntime{
		RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(),
		transport:            transport,
	}
	policy := exchange.DefaultWorkspacePolicy()
	policy.Compatibility = compat.CompatibilityPolicy{Mode: mode}
	ingress := exchange.NewIngress(
		imageIncidentWorkspaceLookup{workspace: workspace},
		runtime,
		exchange.RuntimePoliciesSpec{
			PolicyResolver: exchange.StaticWorkspacePolicyResolver{Policy: policy},
		},
	)
	return NewHandler(ingress, nil)
}

func imageIncidentWorkspace(t *testing.T) routing.Workspace {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug("personal")
	routeName, err := routing.ParseRouteName("image-route")
	if err != nil {
		t.Fatal(err)
	}
	targetID, _ := routing.ParseTargetID("chat-image")
	model, _ := routing.ParseUpstreamModel("chat-model")
	connection, _ := routing.NewCustomConnection("https://example.test/v1", nil)
	protocol, err := routing.ParseProtocol("chat_completions", routing.ProviderCustom, func(routing.Provider, string) bool { return true })
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

func imageIncidentResponsesRequest() string {
	return `{
		"model":"default",
		"reasoning":{"context":"all_turns"},
		"tools":[{"type":"function","name":"view_image","parameters":{"type":"object"}}],
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect"}]},
			{"type":"function_call","call_id":"call_image","name":"view_image","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_image","output":[
				{"type":"input_image","image_url":"data:image/png;base64,` + imageIncidentPNG + `"}
			]},
			{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"seen"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]
	}`
}
