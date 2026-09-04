package compactifai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{ calls int }

func (r *credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	r.calls++
	return "compactifai-token", nil
}

func TestDiscoveryProjectsDocumentedModelCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" || request.Method != http.MethodGet {
			t.Fatalf("catalog request = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer compactifai-token" {
			t.Fatalf("Authorization = %q", got)
		}
		_, _ = io.WriteString(w, `{"data":[
			{"id":"both","owned_by":"zai-org","capabilities":{"support_chat_completion":true,"supports_responses":true}},
			{"id":"chat","owned_by":"compactif","capabilities":{"support_chat_completion":true,"supports_responses":false}},
			{"id":"responses","owned_by":"compactif","capabilities":{"support_chat_completion":false,"supports_responses":true}},
			{"id":"audio","owned_by":"compactif","capabilities":{"support_chat_completion":false,"supports_responses":false}}
		]}`)
	}))
	defer server.Close()

	resolver := &credentialResolver{}
	result, err := NewRuntime(server.Client(), resolver).Discovery.ProbeTarget(context.Background(), compactifAITarget(server.URL+"/v1", "both", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery()))
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 {
		t.Fatalf("credential resolutions = %d", resolver.calls)
	}
	if len(result.Options) != 3 {
		t.Fatalf("catalog options = %#v", result.Options)
	}
	byName := make(map[string]profile.ModelAuthoringOption, len(result.Options))
	for _, option := range result.Options {
		byName[option.Name] = option
	}
	assertModelProtocols(t, byName["both"], "zai-org", []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream"}, "chat_completions")
	assertModelProtocols(t, byName["chat"], "compactif", []string{"chat_completions", "chat_completions_stream"}, "chat_completions")
	assertModelProtocols(t, byName["responses"], "compactif", []string{"responses", "responses_stream"}, "responses")
	if _, exists := byName["audio"]; exists {
		t.Fatal("protocol-less catalog row became an LLM authoring option")
	}
}

func TestDiscoveryFailsMalformedCapabilityMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"bad","capabilities":{"support_chat_completion":"yes"}}]}`)
	}))
	defer server.Close()
	_, err := NewRuntime(server.Client(), &credentialResolver{}).Discovery.ProbeTarget(context.Background(), compactifAITarget(server.URL+"/v1", "bad", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery()))
	if err == nil {
		t.Fatal("malformed CompactifAI capability metadata was approximated")
	}
}

func TestLiveCatalogEvidenceProjectsAccountVisibleCapabilities(t *testing.T) {
	raw, err := os.ReadFile("testdata/characterization/compactifai-model-capabilities-live-2026-09-04.json")
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Schema         string            `json:"schema"`
		ProviderSpec   string            `json:"provider_spec"`
		Endpoint       string            `json:"endpoint"`
		Source         string            `json:"source"`
		Authentication string            `json:"authentication"`
		ExposesCost    bool              `json:"catalog_exposes_cost_or_free_status"`
		Rows           []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Schema != "swobu.provider-characterization/v1" || evidence.ProviderSpec != string(profile.ProviderSpecCompactifAI) || evidence.Endpoint != "https://api.compactif.ai/v1/models" || evidence.Source != "live" || evidence.Authentication != "bearer" || evidence.ExposesCost {
		t.Fatalf("unexpected characterization identity: %#v", evidence)
	}
	projected := make(map[string]profile.ModelAuthoringOption)
	for _, rawRow := range evidence.Rows {
		rows, err := modelRowsFromRaw(rawRow)
		if err != nil {
			t.Fatal(err)
		}
		option, include, err := projectModel(profile.ProviderSpecCompactifAI, rows[0])
		if err != nil {
			t.Fatal(err)
		}
		if include {
			projected[option.Name] = option
		}
	}
	if len(projected) != len(evidence.Rows)-1 {
		t.Fatalf("projected rows = %d, evidence rows = %d", len(projected), len(evidence.Rows))
	}
	if _, exists := projected["cai-whisper-large-v3-turbo-slim"]; exists {
		t.Fatal("live audio-only row became an LLM authoring option")
	}
	assertModelProtocols(t, projected["glm-5-2"], "zai-org", []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream"}, "chat_completions")
	assertModelProtocols(t, projected["quasar-438b"], "multiverse_computing", []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream"}, "chat_completions")
}

func TestChatToolLoopPreservesReadableReasoningWithoutReplay(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestCount++
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("Chat path = %q", request.URL.Path)
		}
		body, _ := io.ReadAll(request.Body)
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if requestCount == 1 {
			_, _ = io.WriteString(w, "data: {\"id\":\"chat_tool\",\"model\":\"quasar-test\",\"choices\":[{\"delta\":{\"reasoning_content\":\"inspect \"}}]}\n\n"+
				"data: {\"id\":\"chat_tool\",\"model\":\"quasar-test\",\"choices\":[{\"delta\":{\"reasoning_content\":\"source\",\"tool_calls\":[{\"index\":0,\"id\":\"call_lookup\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\"}}]}}]}\n\n"+
				"data: {\"id\":\"chat_tool\",\"model\":\"quasar-test\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"x\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"+
				"data: [DONE]\n\n")
			return
		}
		messages := string(payload["messages"])
		if !strings.Contains(messages, `"tool_call_id":"call_lookup"`) || !strings.Contains(messages, `"content":"found"`) || strings.Contains(messages, "reasoning_content") {
			t.Fatalf("second request history = %s", messages)
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"chat_final\",\"model\":\"quasar-test\",\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	target := compactifAITarget(server.URL+"/v1", "quasar-test", protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	backend, err := NewRuntime(server.Client(), &credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	base := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{
		canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object","properties":{"q":{"type":"string"}}}`), canonical.Unspecified[bool]())),
		canonicaltest.Message(t, canonical.MessageRoleUser, "look it up"),
	}})
	firstItems := sendAndProject(t, backend, target, base, "ex_compactifai_tool_1")
	if len(firstItems) != 2 {
		t.Fatalf("reasoning/tool items = %#v", firstItems)
	}
	assertReadableReasoning(t, firstItems[0], "inspect source")
	call, ok := firstItems[1].ToolCall()
	if !ok || call.CallID().String() != "call_lookup" || call.Tool() != key {
		t.Fatalf("fragmented tool call = %#v", firstItems[1])
	}
	result, err := canonical.NewToolResultItem(call.CallID(), []canonical.ToolResultPart{canonical.NewTextToolResultPart("found")}, false)
	if err != nil {
		t.Fatal(err)
	}
	second := base.WithItems(append(base.Items(), firstItems[0], firstItems[1], result))
	finalItems := sendAndProject(t, backend, target, second, "ex_compactifai_tool_2")
	if len(finalItems) != 1 {
		t.Fatalf("final items = %#v", finalItems)
	}
	message, ok := finalItems[0].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("final message = %#v", finalItems[0])
	}
	text, ok := message.Content()[0].Text()
	if !ok || text.Text() != "done" {
		t.Fatalf("final message content = %#v", message.Content())
	}
}

func TestResponsesUsesSharedOpenAIFamilyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("Responses path = %q", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer compactifai-token" {
			t.Fatalf("Authorization = %q", got)
		}
		_, _ = io.WriteString(w, `{"id":"resp_1","object":"response","model":"responses-test","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}]}`)
	}))
	defer server.Close()
	target := compactifAITarget(server.URL+"/v1", "responses-test", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	backend, err := NewRuntime(server.Client(), &credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
	items := sendAndProject(t, backend, target, request, "ex_compactifai_responses")
	if len(items) != 1 {
		t.Fatalf("Responses items = %#v", items)
	}
}

func TestResponsesStreamKeepsFirstObservedOutputIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"quasar-test\",\"status\":\"in_progress\",\"output\":[]}}\n\n"+
			"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"wire_message\",\"type\":\"message\",\"status\":\"in_progress\",\"role\":\"assistant\",\"content\":[]}}\n\n"+
			"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"wire_message\",\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"quasar-test\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_rewritten\",\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}]}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	target := compactifAITarget(server.URL+"/v1", "quasar-test", protocolkind.Responses, "responses_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	backend, err := NewRuntime(server.Client(), &credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
	items := sendAndProject(t, backend, target, request, "ex_compactifai_responses_stream")
	if len(items) != 1 {
		t.Fatalf("Responses stream items = %#v", items)
	}
}

func assertModelProtocols(t *testing.T, option profile.ModelAuthoringOption, publisher string, protocols []string, defaultProtocol string) {
	t.Helper()
	if option.ModelPublisher != publisher || !slices.Equal(option.SupportedProviderProtocols, protocols) || option.DefaultProviderProtocol != defaultProtocol {
		t.Fatalf("model option = %#v", option)
	}
}

func sendAndProject(t *testing.T, backend provider.Backend, target provider.TargetSnapshot, request canonical.CanonicalRequest, exchangeID string) []canonical.CanonicalItem {
	t.Helper()
	toolNames, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	attempt := provider.Request{Attempt: provider.AttemptContext{ExchangeID: exchangeID}, Canonical: request, ToolNames: toolNames, Delivery: target.ProviderDelivery}
	document, _, err := backend.Codec.Encode(attempt)
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := backend.Transport.Send(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.Codec.Decode(context.Background(), attempt, ingress)
	if err != nil {
		t.Fatal(err)
	}
	binding := canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("resp_" + exchangeID), TargetID: target.TargetID, TargetVersion: target.TargetVersion}
	response, err := canonical.ProjectStream(context.Background(), canonical.NewBoundResponseIdentityStream(decoded.Stream, binding), binding)
	if err != nil {
		t.Fatal(err)
	}
	return response.Items()
}

func assertReadableReasoning(t *testing.T, item canonical.CanonicalItem, want string) {
	t.Helper()
	reasoning, ok := item.Reasoning()
	if !ok || len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Kind() != canonical.ReasoningPartTrace || reasoning.Parts()[0].Text() != want {
		t.Fatalf("reasoning item = %#v", item)
	}
	if !reasoning.Opaque().IsZero() {
		t.Fatalf("readable CompactifAI reasoning acquired replay state: %#v", reasoning.Opaque())
	}
}

func compactifAITarget(baseURL, model string, kind protocolkind.ProtocolKind, protocol string, mode delivery.Delivery) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("compactifai", string(profile.ProviderSpecCompactifAI), baseURL, "env:COMPACTIFAI_API_KEY", kind, protocol, mode)
	target.Model = model
	return target
}

func modelRowsFromRaw(raw json.RawMessage) ([]modelcatalogopenai.ModelRow, error) {
	return modelcatalogopenai.DecodeModelRows(strings.NewReader(`{"data":[` + string(raw) + `]}`))
}
