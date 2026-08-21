package mistral

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "mistral-token", nil
}

func TestProfileOwnsStandardRegionalAuthoringAndDerivedChat(t *testing.T) {
	manifest, ok := profile.ProfileForSpec(string(profile.ProviderSpecMistral))
	if !ok {
		t.Fatal("Mistral profile is missing")
	}
	if manifest.ConnectionShape != routing.ConnectionShapeStandard {
		t.Fatalf("connection shape = %v, want Standard", manifest.ConnectionShape)
	}
	if manifest.ProviderDisplayName != "Mistral AI" || manifest.Locator.Kind != profile.LocatorBaseURL || manifest.Locator.Default != "https://api.mistral.ai/v1" {
		t.Fatalf("manifest locator/display = %#v", manifest)
	}
	if manifest.Credential.Requirement != profile.CredentialRequired || manifest.Credential.SuggestedEnvVar != "MISTRAL_API_KEY" {
		t.Fatalf("credential = %#v", manifest.Credential)
	}
	if manifest.ModelDiscovery != profile.ModelDiscoveryModeAdvisory {
		t.Fatalf("catalog = %v", manifest.ModelDiscovery)
	}
	protocol, derived := profile.DerivedProtocolForSpec(string(profile.ProviderSpecMistral))
	if !derived || protocol != "chat_completions_stream" {
		t.Fatalf("derived protocol = %q, %v", protocol, derived)
	}
	protocols := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecMistral))
	if !reflect.DeepEqual(protocols, []string{"chat_completions_stream"}) {
		t.Fatalf("protocols = %v", protocols)
	}
}

func TestRuntimeUsesSharedChatPathBearerAuthAndOptionalSemantics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer mistral-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"messages", "tools", "response_format"} {
			if len(payload[field]) == 0 {
				t.Fatalf("shared Chat field %q missing: %#v", field, payload)
			}
		}
		if !bytes.Contains(payload["messages"], []byte("9007199254740993")) || !bytes.Contains(payload["tools"], []byte("9007199254740993")) || !bytes.Contains(payload["response_format"], []byte("9007199254740993")) {
			t.Fatalf("shared optional semantics changed during Mistral encoding: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	target := mistralTarget(server.URL + "/v1")
	backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	if codec, ok := backend.Codec.(protocolcodec.Codec); !ok || codec.ChatDialect.ResponseReasoning == nil {
		t.Fatalf("codec = %T, want Mistral reasoning codec", backend.Codec)
	}
	request := canonicaltest.LargeIntegerRequest(t, target.Model)
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err != nil {
		t.Fatal(err)
	}

	for _, kind := range []protocolkind.ProtocolKind{protocolkind.Responses, protocolkind.Messages} {
		invalid := target.Clone()
		invalid.ProtocolKind = kind
		if _, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(invalid); err == nil {
			t.Fatalf("%s must not resolve as a Mistral backend", kind)
		}
	}
}

func TestDiscoveryUsesEffectiveBaseAndFiltersOnlyAuthoringMetadata(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.URL.Path != "/regional/v1/models" {
			t.Fatalf("catalog path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer mistral-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[
			{"id":"chat-active","archived":false,"capabilities":{"completion_chat":true}},
			{"id":"chat-archived","archived":true,"capabilities":{"completion_chat":true}},
			{"id":"embed-active","archived":false,"capabilities":{"completion_chat":false}},
			{"id":"","archived":false,"capabilities":{"completion_chat":true}}
		]}`))
	}))
	defer server.Close()

	target := mistralTarget(server.URL + "/regional/v1")
	result, err := NewRuntime(server.Client(), credentialResolver{}).Discovery.ProbeTarget(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(requests, []string{"/regional/v1/models"}) {
		t.Fatalf("catalog requests = %v", requests)
	}
	if len(result.Options) != 1 || result.Options[0].Name != "chat-active" || result.Options[0].ModelName != "chat-active" {
		t.Fatalf("deployments = %#v", result.Options)
	}
}

func TestBufferedThinkChunksNormalizeToReasoningVisibleTextAndReplay(t *testing.T) {
	document := carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(`{
		"choices":[{"message":{"role":"assistant","content":[
			{"type":"thinking","thinking":[{"type":"text","text":"A"},{"type":"text","text":" C"}]},
			{"type":"text","text":"B"}
		]},"finish_reason":"stop"}]
	}`), carrier.Meta{})
	cleaned, item, err := protocolcodec.ExtractChatReasoningDocument(document, mistralChatReasoningExtractor{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned.RawBytes(), []byte(`"type":"thinking"`)) || !bytes.Contains(cleaned.RawBytes(), []byte(`"content":"B"`)) {
		t.Fatalf("normalized response = %s", cleaned.RawBytes())
	}
	reasoning, ok := item.Reasoning()
	if !ok || len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Text() != "A C" {
		t.Fatalf("reasoning item = %#v", item)
	}
	opaque, ok := reasoning.Opaque().ProviderChat(ChatReplayScope)
	if !ok || string(opaque) != "A C" {
		t.Fatalf("opaque replay = %q, %v", opaque, ok)
	}
}

func TestStreamedThinkChunksNormalizeAtVisibleTransition(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":[{\"type\":\"thinking\",\"thinking\":[{\"type\":\"text\",\"text\":\"A \"}]}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":[{\"type\":\"thinking\",\"thinking\":[{\"type\":\"text\",\"text\":\"C\"}]},{\"type\":\"text\",\"text\":\"B\"}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"D\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	body := protocolcodec.NewChatReasoningSSEBody(io.NopCloser(strings.NewReader(stream)), mistralChatReasoningExtractor{})
	cleaned, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned, []byte(`"type":"thinking"`)) || !bytes.Contains(cleaned, []byte(`"content":"B"`)) || !bytes.Contains(cleaned, []byte(`"content":"D"`)) {
		t.Fatalf("normalized stream = %s", cleaned)
	}
	item, ok := body.Take()
	if !ok {
		t.Fatal("streamed reasoning item missing")
	}
	reasoning, _ := item.Reasoning()
	opaque, _ := reasoning.Opaque().ProviderChat(ChatReplayScope)
	if reasoning.Parts()[0].Text() != "A C" || string(opaque) != "A C" {
		t.Fatalf("streamed reasoning = %#v, opaque=%q", reasoning.Parts(), opaque)
	}
}

func TestUnknownMistralContentChunkFailsInsteadOfDropping(t *testing.T) {
	for name, content := range map[string]string{
		"unknown outer chunk":   `[{"type":"future","value":"do not drop"}]`,
		"unknown thinking part": `[{"type":"thinking","thinking":[{"type":"future","text":"do not drop"}]}]`,
	} {
		t.Run(name, func(t *testing.T) {
			message := map[string]json.RawMessage{"content": json.RawMessage(content)}
			if _, err := (mistralChatReasoningExtractor{}).ExtractBufferedChatReasoning(message); err == nil {
				t.Fatal("unknown Mistral content was silently accepted")
			}
		})
	}
}

func TestMistralReplayPrependsThinkChunkToTextAndToolCallTurns(t *testing.T) {
	reasoning := mistralReasoning(t, ChatReplayScope, "private reasoning")
	textRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("mistral-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "question"),
			reasoning,
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer"),
		},
	})
	bundle := NewRuntime(nil, credentialResolver{})
	backend, err := bundle.BackendResolver.ResolveBackend(mistralTarget("https://api.mistral.ai/v1"))
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: textRequest, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	assertMistralReplayMessage(t, document.RawBytes(), 1, true, false)

	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	declaration := canonicaltest.MustFunctionTool(key, "", canonicaltest.MustSchema(`{"type":"object"}`), canonical.Unspecified[bool]())
	toolRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("mistral-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, declaration),
			canonicaltest.Message(t, canonical.MessageRoleUser, "question"),
			reasoning,
			canonicaltest.ToolCall(t, "call_lookup", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`))),
		},
	})
	names, _, err := provider.BuildAttemptToolNames(toolRequest)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err = backend.Codec.Encode(provider.Request{Canonical: toolRequest, ToolNames: names, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	assertMistralReplayMessage(t, document.RawBytes(), 1, false, true)
}

func TestMistralReplayRejectsDuplicateAndIgnoresForeignOpaqueState(t *testing.T) {
	bundle := NewRuntime(nil, credentialResolver{})
	backend, err := bundle.BackendResolver.ResolveBackend(mistralTarget("https://api.mistral.ai/v1"))
	if err != nil {
		t.Fatal(err)
	}
	duplicate := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("mistral-model"),
		Items: []canonical.CanonicalItem{
			mistralReasoning(t, ChatReplayScope, "one"),
			mistralReasoning(t, ChatReplayScope, "two"),
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer"),
		},
	})
	if _, _, err := backend.Codec.Encode(provider.Request{Canonical: duplicate, Delivery: delivery.BufferedDelivery()}); err == nil {
		t.Fatal("duplicate Mistral replay state must fail")
	}

	foreign := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("mistral-model"),
		Items: []canonical.CanonicalItem{
			mistralReasoning(t, "foreign-chat", "foreign"),
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer"),
		},
	})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: foreign, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(document.RawBytes(), []byte(`"type":"thinking"`)) {
		t.Fatalf("foreign replay leaked to Mistral: %s", document.RawBytes())
	}
}

func TestMistralEffortProjectionIncludesBoundedMaxApproximation(t *testing.T) {
	disabled, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: canonical.Specify(canonical.NewDisabledReasoningCompute()),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertMistralEffort(t, canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Reasoning: disabled}), "none", false)

	for _, test := range []struct {
		effort canonical.InferenceEffort
		want   string
		approx bool
	}{
		{canonical.InferenceEffortMinimal, "minimal", false},
		{canonical.InferenceEffortLow, "low", false},
		{canonical.InferenceEffortMedium, "medium", false},
		{canonical.InferenceEffortHigh, "high", false},
		{canonical.InferenceEffortXHigh, "xhigh", false},
		{canonical.InferenceEffortMax, "xhigh", true},
	} {
		controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &test.effort})
		if err != nil {
			t.Fatal(err)
		}
		request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Controls: controls})
		assertMistralEffort(t, request, test.want, test.approx)
	}
}

func assertMistralEffort(t *testing.T, request canonical.CanonicalRequest, want string, wantApprox bool) {
	t.Helper()
	bundle := NewRuntime(nil, credentialResolver{})
	backend, err := bundle.BackendResolver.ResolveBackend(mistralTarget("https://api.mistral.ai/v1"))
	if err != nil {
		t.Fatal(err)
	}
	document, changes, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["reasoning_effort"] != want {
		t.Fatalf("reasoning_effort = %#v, want %q; document=%s", payload["reasoning_effort"], want, document.RawBytes())
	}
	gotApprox := false
	for _, change := range changes {
		if change.Capability == canonical.RequestControlsEffort && change.Kind == compat.Approximation && change.Preserved == canonical.RequestControlsEffort {
			gotApprox = true
		}
	}
	if gotApprox != wantApprox {
		t.Fatalf("effort approximation = %v, want %v; changes=%#v", gotApprox, wantApprox, changes)
	}
}

func assertMistralReplayMessage(t *testing.T, raw []byte, index int, wantText, wantCalls bool) {
	t.Helper()
	var payload struct {
		Messages []struct {
			Content   json.RawMessage   `json:"content"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if index >= len(payload.Messages) || len(payload.Messages[index].Content) == 0 {
		t.Fatalf("replay message missing: %s", raw)
	}
	var content []json.RawMessage
	if err := json.Unmarshal(payload.Messages[index].Content, &content); err != nil {
		t.Fatalf("replay content = %s: %v", payload.Messages[index].Content, err)
	}
	var thinking mistralThinkingChunk
	if err := json.Unmarshal(content[0], &thinking); err != nil {
		t.Fatal(err)
	}
	if thinking.Type != "thinking" || len(thinking.Thinking) != 1 || thinking.Thinking[0].Type != "text" || thinking.Thinking[0].Text != "private reasoning" {
		t.Fatalf("thinking chunk = %#v", thinking)
	}
	if wantText {
		if len(content) != 2 {
			t.Fatalf("text replay content = %#v", content)
		}
		var text mistralTextChunk
		if err := json.Unmarshal(content[1], &text); err != nil {
			t.Fatal(err)
		}
		if text.Type != "text" || text.Text != "answer" {
			t.Fatalf("text chunk = %#v", text)
		}
	}
	if (len(payload.Messages[index].ToolCalls) > 0) != wantCalls {
		t.Fatalf("tool calls present = %v, want %v; message=%#v", len(payload.Messages[index].ToolCalls) > 0, wantCalls, payload.Messages[index])
	}
}

func mistralReasoning(t *testing.T, scope canonical.ProviderChatReplayScope, text string) canonical.CanonicalItem {
	t.Helper()
	opaque, err := canonical.NewProviderChatOpaqueThinking(scope, []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, text)
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func mistralTarget(baseURL string) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("mistral", string(profile.ProviderSpecMistral), baseURL, "env:MISTRAL_API_KEY", protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = "mistral-model"
	return target
}
