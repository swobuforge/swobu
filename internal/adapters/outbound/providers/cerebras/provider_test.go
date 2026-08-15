package cerebras

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
	return "cerebras-token", nil
}

func TestProfileOwnsFixedStandardConnectionAndDerivedStreamingChat(t *testing.T) {
	manifest, ok := profile.ProfileForSpec(string(profile.ProviderSpecCerebras))
	if !ok {
		t.Fatal("Cerebras profile is missing")
	}
	if manifest.ProviderDisplayName != "Cerebras" || manifest.ConnectionShape != routing.ConnectionShapeStandard {
		t.Fatalf("display/shape = %q/%v", manifest.ProviderDisplayName, manifest.ConnectionShape)
	}
	if manifest.Locator.Kind != profile.LocatorFixed || manifest.Locator.Default != "https://api.cerebras.ai/v1" {
		t.Fatalf("locator = %#v", manifest.Locator)
	}
	if manifest.Credential.Requirement != profile.CredentialRequired || manifest.Credential.SuggestedEnvVar != "CEREBRAS_API_KEY" {
		t.Fatalf("credential = %#v", manifest.Credential)
	}
	if manifest.ModelDiscovery != profile.ModelDiscoveryModeAdvisory {
		t.Fatalf("catalog = %v", manifest.ModelDiscovery)
	}
	protocol, derived := profile.DerivedProtocolForSpec(string(profile.ProviderSpecCerebras))
	if !derived || protocol != "chat_completions_stream" || !reflect.DeepEqual(profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecCerebras)), []string{"chat_completions_stream"}) {
		t.Fatalf("derived protocol = %q, %v", protocol, derived)
	}
}

func TestSharedDiscoveryAndChatPreserveDedicatedModelWithoutVersionHeader(t *testing.T) {
	const dedicatedModel = "my-org-gpt-oss-120b"
	var catalogCalls, chatCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cerebras-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Cerebras-Version-Patch") != "" {
			t.Fatalf("obsolete version header = %q", r.Header.Get("X-Cerebras-Version-Patch"))
		}
		switch r.URL.Path {
		case "/v1/models":
			catalogCalls++
			_, _ = w.Write([]byte(`{"data":[{"id":"shared-model"},{"id":"second-model"}]}`))
		case "/v1/chat/completions":
			chatCalls++
			var payload struct {
				Model  string `json:"model"`
				Stream bool   `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Model != dedicatedModel || !payload.Stream {
				t.Fatalf("model/stream = %q/%v", payload.Model, payload.Stream)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	target := cerebrasTarget(server.URL+"/v1", dedicatedModel)
	bundle := NewRuntime(server.Client(), credentialResolver{})
	result, err := bundle.Discovery.ProbeTarget(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Options) != 2 || result.Options[0].Name != "second-model" || result.Options[1].Name != "shared-model" {
		t.Fatalf("generic catalog = %#v", result.Options)
	}
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(dedicatedModel), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err != nil {
		t.Fatal(err)
	}
	if catalogCalls != 1 || chatCalls != 1 {
		t.Fatalf("catalog/chat calls = %d/%d", catalogCalls, chatCalls)
	}
}

func TestBufferedAndStreamedReasoningBecomeScopedReplay(t *testing.T) {
	document := carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(`{"choices":[{"message":{"role":"assistant","reasoning":"private thought","content":"answer"},"finish_reason":"stop"}]}`), carrier.Meta{})
	cleaned, item, err := protocolcodec.ExtractChatReasoningDocument(document, cerebrasChatReasoningExtractor{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned.RawBytes(), []byte(`"reasoning"`)) || !bytes.Contains(cleaned.RawBytes(), []byte(`"content":"answer"`)) {
		t.Fatalf("cleaned response = %s", cleaned.RawBytes())
	}
	assertCerebrasReasoning(t, item, "private thought")

	stream := "data: {\"choices\":[{\"delta\":{\"reasoning\":\"private \"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"reasoning\":\"thought\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	body := protocolcodec.NewChatReasoningSSEBody(io.NopCloser(strings.NewReader(stream)), cerebrasChatReasoningExtractor{})
	cleanedStream, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleanedStream, []byte(`"reasoning"`)) || !bytes.Contains(cleanedStream, []byte(`"content":"answer"`)) {
		t.Fatalf("cleaned stream = %s", cleanedStream)
	}
	item, ok := body.Take()
	if !ok {
		t.Fatal("streamed reasoning item missing")
	}
	assertCerebrasReasoning(t, item, "private thought")
}

func TestReasoningReplaysOnTextAndToolCallAssistantMessages(t *testing.T) {
	reasoning := cerebrasReasoning(t, ChatReplayScope, "private thought")
	textRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "question"), reasoning,
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer"),
		},
	})
	document, _, err := (reasoningCodec{}).Encode(provider.Request{Canonical: textRequest, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	assertCerebrasReplay(t, document.RawBytes(), 1, false)

	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	declaration := canonicaltest.MustFunctionTool(key, "", canonicaltest.MustSchema(`{"type":"object"}`), canonical.Unspecified[bool]())
	toolRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, declaration),
			canonicaltest.Message(t, canonical.MessageRoleUser, "question"), reasoning,
			canonicaltest.ToolCall(t, "call_lookup", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`))),
		},
	})
	names, _, err := provider.BuildAttemptToolNames(toolRequest)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err = (reasoningCodec{}).Encode(provider.Request{Canonical: toolRequest, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	assertCerebrasReplay(t, document.RawBytes(), 1, true)
}

func TestReplayIgnoresForeignScopeAndRejectsDuplicateCerebrasState(t *testing.T) {
	foreign := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{cerebrasReasoning(t, "foreign-chat", "foreign"), canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer")},
	})
	document, _, err := (reasoningCodec{}).Encode(provider.Request{Canonical: foreign, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(document.RawBytes(), []byte(`"reasoning"`)) {
		t.Fatalf("foreign replay leaked: %s", document.RawBytes())
	}

	duplicate := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			cerebrasReasoning(t, ChatReplayScope, "one"), cerebrasReasoning(t, ChatReplayScope, "two"),
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer"),
		},
	})
	if _, _, err := (reasoningCodec{}).Encode(provider.Request{Canonical: duplicate, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}); err == nil {
		t.Fatal("duplicate Cerebras replay state must fail")
	}
}

func TestProviderSpecificCombinationReachesBackendWithoutCapabilityPreflight(t *testing.T) {
	dispatched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatched = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"selected model rejects this combination"}}`))
	}))
	defer server.Close()

	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONObject})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        canonical.Specify("unknown-dedicated-endpoint"),
		Items:        []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "respond as JSON")},
		OutputFormat: canonical.Specify(format),
	})
	backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(cerebrasTarget(server.URL, request.Model()))
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatalf("local capability preflight rejected request: %v", err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err == nil {
		t.Fatal("provider rejection was not returned")
	}
	if !dispatched {
		t.Fatal("request never reached Cerebras backend")
	}
}

func assertCerebrasReasoning(t *testing.T, item canonical.CanonicalItem, want string) {
	t.Helper()
	reasoning, ok := item.Reasoning()
	if !ok || len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Text() != want {
		t.Fatalf("reasoning item = %#v", item)
	}
	opaque, ok := reasoning.Opaque().ProviderChat(ChatReplayScope)
	if !ok || string(opaque) != want {
		t.Fatalf("opaque replay = %q, %v", opaque, ok)
	}
}

func assertCerebrasReplay(t *testing.T, raw []byte, index int, wantToolCall bool) {
	t.Helper()
	var payload struct {
		Messages []struct {
			Reasoning string            `json:"reasoning"`
			Content   json.RawMessage   `json:"content"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if index >= len(payload.Messages) || payload.Messages[index].Reasoning != "private thought" {
		t.Fatalf("replay message = %#v; raw=%s", payload.Messages, raw)
	}
	if (len(payload.Messages[index].ToolCalls) > 0) != wantToolCall {
		t.Fatalf("tool-call replay = %#v", payload.Messages[index])
	}
}

func cerebrasReasoning(t *testing.T, scope canonical.ProviderChatReplayScope, text string) canonical.CanonicalItem {
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

func cerebrasTarget(baseURL, model string) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("cerebras", string(profile.ProviderSpecCerebras), baseURL, "env:CEREBRAS_API_KEY", protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = model
	return target
}
