package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/cachelocality"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/testkit/providertest"
)

func TestOpenRouterAutomaticReasoningIsExact(t *testing.T) {
	reasoning, _ := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(canonical.NewAutomaticReasoningCompute())})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Reasoning: reasoning})
	backend := openRouterBackend(t, request.Model())
	document, changes, err := backend.Codec.Encode(provider.Request{
		Canonical: request, Delivery: delivery.BufferedDelivery(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"reasoning":{"enabled":true}`) {
		t.Fatalf("automatic reasoning = %s", document.RawBytes())
	}
	if len(changes) != 0 {
		t.Fatalf("exact reasoning lowering changes = %#v", changes)
	}
}

func TestOpenRouterEmitsBoundedOpaqueSessionAcrossProtocols(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})
	values := map[protocolkind.ProtocolKind]string{}
	for _, protocol := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses} {
		backend := openRouterBackendForProtocol(t, request.Model(), protocol)
		document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, CacheLocality: cachelocality.Explicit("client-secret"), Delivery: delivery.BufferedDelivery()})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
			t.Fatal(err)
		}
		sessionID, _ := payload["session_id"].(string)
		if !strings.HasPrefix(sessionID, "swobu_") || len(sessionID) > 256 || strings.Contains(sessionID, "client-secret") {
			t.Fatalf("session_id = %q", sessionID)
		}
		const want = "swobu_d6deb586a94ed79f6f18772d40e4b3fb9e120991cecb025827e3adc3d0b199ad"
		if sessionID != want {
			t.Fatalf("session_id = %q, want %q", sessionID, want)
		}
		values[protocol] = sessionID
	}
	if values[protocolkind.ChatCompletions] != values[protocolkind.Responses] {
		t.Fatalf("session ids differ across protocols: %#v", values)
	}
	backend := openRouterBackendForProtocol(t, request.Model(), protocolkind.ChatCompletions)
	different, _, err := backend.Codec.Encode(provider.Request{Canonical: request, CacheLocality: cachelocality.Explicit("different"), Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	var differentPayload map[string]any
	if err := json.Unmarshal(different.RawBytes(), &differentPayload); err != nil {
		t.Fatal(err)
	}
	if differentPayload["session_id"] == values[protocolkind.ChatCompletions] {
		t.Fatal("different cache localities produced the same OpenRouter session_id")
	}
	without, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	var zeroPayload map[string]any
	if err := json.Unmarshal(without.RawBytes(), &zeroPayload); err != nil {
		t.Fatal(err)
	}
	if _, exists := zeroPayload["session_id"]; exists {
		t.Fatalf("zero cache locality emitted session_id: %s", without.RawBytes())
	}
}

func TestOpenRouterCacheSensitiveRenderingIgnoresExecutionContext(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})
	for _, protocol := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses} {
		backend := openRouterBackendForProtocol(t, request.Model(), protocol)
		project := func(exchangeID, locality string, d delivery.Delivery) []byte {
			document, _, err := backend.Codec.Encode(provider.Request{ExchangeID: exchangeID, Canonical: request, CacheLocality: cachelocality.Explicit(locality), Delivery: d})
			if err != nil {
				t.Fatal(err)
			}
			projection, err := providertest.CacheSensitiveProjection(document)
			if err != nil {
				t.Fatal(err)
			}
			return projection
		}
		first := project("a", "locality-a", delivery.BufferedDelivery())
		second := project("b", "locality-b", delivery.BufferedDelivery())
		streamed := project("c", "locality-c", delivery.StreamingDelivery(delivery.FramingSSE))
		if !bytes.Equal(first, second) || !bytes.Equal(first, streamed) {
			t.Fatalf("OpenRouter %s cache-sensitive projection changed: %s / %s / %s", protocol, first, second, streamed)
		}
	}
}

func TestOpenRouterRequestMutationPreservesRawJSONIntegers(t *testing.T) {
	request := canonicaltest.LargeIntegerRequest(t, "model")
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	backend := openRouterBackend(t, request.Model())
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(document.RawBytes(), []byte("9007199254740993")); got != 3 {
		t.Fatalf("large integer occurrences = %d, want 3: %s", got, document.RawBytes())
	}
}

func TestOpenRouterOwnsFinalWebSearchDialectAcrossProtocols(t *testing.T) {
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	if err != nil {
		t.Fatal(err)
	}
	webSearchKey := canonical.WebSearchToolKey()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, set.Declarations()...),
			canonicaltest.Message(t, canonical.MessageRoleUser, "search"),
		},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &webSearchKey)),
	})
	for _, protocol := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses} {
		t.Run(string(protocol), func(t *testing.T) {
			backend := openRouterBackendForProtocol(t, request.Model(), protocol)
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(document.RawBytes(), []byte(`"type":"openrouter:web_search"`)) {
				t.Fatalf("final OpenRouter JSON = %s", document.RawBytes())
			}
			if bytes.Contains(document.RawBytes(), []byte(`"type":"web_search"`)) {
				t.Fatalf("standard marker leaked into OpenRouter JSON = %s", document.RawBytes())
			}
			if bytes.Contains(document.RawBytes(), []byte(`"web_search_options":`)) {
				t.Fatalf("standard web-search options leaked into OpenRouter JSON = %s", document.RawBytes())
			}
			if !bytes.Contains(document.RawBytes(), []byte(`"tool_choice":{"type":"openrouter:web_search"}`)) {
				t.Fatalf("OpenRouter-specific choice missing from JSON = %s", document.RawBytes())
			}
		})
	}
}

func TestOpenRouterResponsesUsesFlatNamespaceGrammar(t *testing.T) {
	childKey, _ := canonical.NewToolKey("workspace", canonical.ToolKindFunction, "read_file")
	child := canonicaltest.MustFunctionTool(childKey, "Read", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	namespaceKey, _ := canonical.NewRequestToolKey(canonical.ToolKindNamespace, "workspace")
	namespace, _ := canonical.NewToolNamespace(namespaceKey, "Workspace", []canonical.ToolDeclaration{child})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, namespace), canonicaltest.Message(t, canonical.MessageRoleUser, "read")},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	backend := openRouterBackendForProtocol(t, "model", protocolkind.Responses)
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	raw := string(document.RawBytes())
	wireName, _ := names.WireName(childKey)
	if strings.Contains(raw, `"type":"namespace"`) || !strings.Contains(raw, `"name":"`+wireName+`"`) {
		t.Fatalf("OpenRouter flat Responses document = %s", raw)
	}
}

func TestOpenRouterResponseTransformsPreserveUnownedRawJSON(t *testing.T) {
	backend := openRouterBackend(t, "model")
	raw := []byte(`{"id":"resp","usage":{"extension":9007199254740993},"choices":[{"message":{"role":"assistant","content":"ok","reasoning":"think","extension":{"constant":9007199254740993}}}]}`)
	document := carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, raw, carrier.Meta{})
	decoded, err := backend.Codec.Decode(context.Background(), provider.Request{ExchangeID: "ex", Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model")})}, provider.DocumentIngress{Document: document})
	if err != nil {
		t.Fatal(err)
	}
	events := drainEvents(t, decoded.Stream)
	if len(events) == 0 {
		t.Fatal("no events decoded")
	}

	streamRaw := "data: {\"choices\":[{\"delta\":{\"reasoning\":\"think\",\"extension\":{\"constant\":9007199254740993}}}]}\n\ndata: [DONE]\n\n"
	streamDecoded, err := backend.Codec.Decode(context.Background(), provider.Request{ExchangeID: "ex", Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model")})}, provider.StreamIngress{Stream: carrier.ByteStream{
		MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(streamRaw)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	streamEvents := drainEvents(t, streamDecoded.Stream)
	if len(streamEvents) == 0 {
		t.Fatal("no stream events decoded")
	}
}

func TestOpenRouterCodecOwnsReasoningRequestAndOpaqueReplay(t *testing.T) {
	effort := canonical.InferenceEffortHigh
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	reasoningControls, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: canonical.Specify(canonical.NewAutomaticReasoningCompute()),
	})
	if err != nil {
		t.Fatal(err)
	}
	opaque, err := canonical.NewProviderChatOpaqueThinking(ChatReplayScope, []byte(`[{"type":"reasoning.summary","summary":"prior"}]`))
	if err != nil {
		t.Fatal(err)
	}
	part, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "prior")
	reasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	assistant := messageItem(t, canonical.MessageRoleAssistant, "answer")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{reasoning, assistant},
		Controls: controls, Reasoning: reasoningControls,
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	backend := openRouterBackend(t, request.Model())
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	wireReasoning, _ := payload["reasoning"].(map[string]any)
	if wireReasoning["enabled"] != true || wireReasoning["effort"] != "high" {
		t.Fatalf("reasoning = %#v", wireReasoning)
	}
	if _, leaked := payload["reasoning_effort"]; leaked {
		t.Fatalf("standard reasoning_effort leaked into OpenRouter request: %s", document.RawBytes())
	}
	messages, _ := payload["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	if _, ok := first["reasoning_details"].([]any); !ok {
		t.Fatalf("opaque reasoning_details were not replayed: %s", document.RawBytes())
	}
}

func TestOpenRouterDisabledReasoningOmitsRedundantEffort(t *testing.T) {
	effort := canonical.InferenceEffortHigh
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	reasoningControls, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: canonical.Specify(canonical.NewDisabledReasoningCompute()),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{messageItem(t, canonical.MessageRoleUser, "hello")},
		Controls: controls, Reasoning: reasoningControls,
	})
	backend := openRouterBackend(t, request.Model())
	document, changes, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(document.RawBytes(), []byte(`"enabled":false`)) || bytes.Contains(document.RawBytes(), []byte(`"effort"`)) {
		t.Fatalf("reasoning payload = %s", document.RawBytes())
	}
	want := compat.NewOmission(canonical.RequestControlsEffort, canonical.Occurrence{})
	if len(changes) != 1 || changes[0] != want {
		t.Fatalf("changes = %#v, want %#v", changes, want)
	}
}

func TestOpenRouterDisabledReasoningUsesEmpiricalFallback(t *testing.T) {
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: canonical.Specify(canonical.NewDisabledReasoningCompute()),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Reasoning: reasoning,
		Items: []canonical.CanonicalItem{messageItem(t, canonical.MessageRoleUser, "hello")},
	})
	facts := provider.NewTargetFacts(func(fact provider.TargetFact) (bool, bool) {
		return false, fact == provider.AcceptsReasoningDisabled
	})
	document, changes, err := openRouterBackend(t, request.Model()).Codec.Encode(provider.Request{
		Canonical: request, Delivery: delivery.BufferedDelivery(), TargetFacts: facts,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := compat.NewOmission(canonical.RequestReasoning, canonical.Occurrence{})
	if bytes.Contains(document.RawBytes(), []byte(`"enabled":false`)) || len(changes) != 1 || changes[0] != want || facts.Reads()[provider.AcceptsReasoningDisabled] {
		t.Fatalf("document=%s changes=%#v reads=%#v", document.RawBytes(), changes, facts.Reads())
	}
}

func TestOpenRouterDoesNotSerializeForeignProviderChatOpaqueThinking(t *testing.T) {
	opaque, err := canonical.NewProviderChatOpaqueThinking("foreign-chat-replay", []byte(`[{"type":"reasoning.summary","summary":"foreign"}]`))
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "foreign")
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{
			reasoning, messageItem(t, canonical.MessageRoleAssistant, "answer"),
		},
	})
	backend := openRouterBackend(t, request.Model())
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(document.RawBytes(), []byte("reasoning_details")) {
		t.Fatalf("foreign opaque state leaked to OpenRouter: %s", document.RawBytes())
	}
}

func TestOpenRouterRejectsDuplicateProviderChatOpaqueThinking(t *testing.T) {
	first := openRouterReasoningItem(t, "first")
	second := openRouterReasoningItem(t, "second")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{first, second, messageItem(t, canonical.MessageRoleAssistant, "answer")},
	})
	backend := openRouterBackend(t, request.Model())
	if _, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()}); err == nil || !strings.Contains(err.Error(), "duplicate provider Chat opaque thinking") {
		t.Fatalf("duplicate OpenRouter replay error = %v", err)
	}
}

func TestOpenRouterOpaqueReplayFollowsMixedAssistantTextAndToolTurns(t *testing.T) {
	reasoningOne := openRouterReasoningItem(t, "first")
	reasoningTwo := openRouterReasoningItem(t, "second")
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	callID, err := canonical.NewToolCallID("call_1")
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{
		canonical.NewTextToolResultPart("result"),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{
			reasoningOne, messageItem(t, canonical.MessageRoleAssistant, "first answer"), call,
			result, reasoningTwo, messageItem(t, canonical.MessageRoleAssistant, "second answer"),
		},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	backend := openRouterBackend(t, request.Model())
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, message := range payload.Messages {
		if message["role"] != "assistant" {
			continue
		}
		details, _ := message["reasoning_details"].([]any)
		if len(details) == 0 {
			got = append(got, "")
			continue
		}
		entry, _ := details[0].(map[string]any)
		got = append(got, entry["summary"].(string))
	}
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("assistant reasoning replay = %#v, want [first second]; document=%s", got, document.RawBytes())
	}
}

func TestOpenRouterBufferedReasoningCompletesAtomicallyBeforeAnswer(t *testing.T) {
	backend := openRouterBackend(t, "model")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model")})
	document := carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(`{
  "id":"chat_1","model":"model","choices":[{"message":{"role":"assistant","reasoning_details":[{"type":"reasoning.summary","summary":"brief"}],"content":"answer"},"finish_reason":"stop"}]
}`), carrier.Meta{})
	decoded, err := backend.Codec.Decode(context.Background(), provider.Request{ExchangeID: "ex", Canonical: request}, provider.DocumentIngress{Document: document})
	if err != nil {
		t.Fatal(err)
	}
	events := drainEvents(t, decoded.Stream)
	reasoningIndex, answerIndex := -1, -1
	for index, event := range events {
		itemEvent, ok := event.Payload.(canonical.ItemEvent)
		if !ok {
			continue
		}
		if completed, ok := itemEvent.Payload.(canonical.ItemCompletedPayload); ok && completed.Item.Kind() == canonical.ItemKindReasoning {
			reasoningIndex = index
			reasoning, _ := completed.Item.Reasoning()
			if _, ok := reasoning.Opaque().ProviderChat(ChatReplayScope); !ok {
				t.Fatal("reasoning item lost OpenRouter opaque replay unit")
			}
		}
		if event.Kind == canonical.EventItemStart && itemEvent.Position.Item == 1 {
			answerIndex = index
		}
	}
	if reasoningIndex < 0 || answerIndex < 0 || reasoningIndex >= answerIndex {
		t.Fatalf("reasoning/answer order = %d/%d; events=%#v", reasoningIndex, answerIndex, events)
	}
}

func TestOpenRouterStreamingBuffersOnlyReasoningArtifact(t *testing.T) {
	backend := openRouterBackend(t, "model")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model")})
	raw := strings.Join([]string{
		`data: {"id":"chat_1","model":"model","choices":[{"delta":{"role":"assistant","reasoning_details":[{"type":"reasoning.text","text":"trace"}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"chat_1","model":"model","choices":[{"delta":{"content":"answer"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chat_1","model":"model","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	decoded, err := backend.Codec.Decode(context.Background(), provider.Request{ExchangeID: "ex", Canonical: request}, provider.StreamIngress{Stream: carrier.ByteStream{
		MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	events := drainEvents(t, decoded.Stream)
	reasoningIndex, textIndex := -1, -1
	for index, event := range events {
		if event.Kind == canonical.EventItemCompleted {
			itemEvent := event.Payload.(canonical.ItemEvent)
			completed := itemEvent.Payload.(canonical.ItemCompletedPayload)
			if completed.Item.Kind() == canonical.ItemKindReasoning {
				reasoningIndex = index
			}
		}
		if event.Kind == canonical.EventTextDelta {
			textIndex = index
		}
	}
	if reasoningIndex < 0 || textIndex < 0 || reasoningIndex >= textIndex {
		t.Fatalf("reasoning/text order = %d/%d; events=%#v", reasoningIndex, textIndex, events)
	}
}

func TestOpenRouterStreamingDoesNotFinalizeOnEmptyContentBeforeReasoning(t *testing.T) {
	backend := openRouterBackend(t, "model")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model")})
	raw := strings.Join([]string{
		`data: {"id":"chat_1","model":"model","choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		"",
		`data: {"id":"chat_1","model":"model","choices":[{"delta":{"reasoning_details":[{"type":"reasoning.summary","summary":"brief"}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"chat_1","model":"model","choices":[{"delta":{"content":"answer"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chat_1","model":"model","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	decoded, err := backend.Codec.Decode(context.Background(), provider.Request{
		ExchangeID: "ex", Canonical: request,
	}, provider.StreamIngress{Stream: carrier.ByteStream{
		MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range drainEvents(t, decoded.Stream) {
		if event.Kind != canonical.EventItemCompleted {
			continue
		}
		itemEvent := event.Payload.(canonical.ItemEvent)
		completed := itemEvent.Payload.(canonical.ItemCompletedPayload)
		if completed.Item.Kind() == canonical.ItemKindReasoning {
			return
		}
	}
	t.Fatal("stream dropped reasoning after an empty content delta")
}

func TestOpenRouterStreamingIgnoresNullReasoningAtTerminalFinish(t *testing.T) {
	backend := openRouterBackend(t, "model")
	raw := strings.Join([]string{
		`data: {"id":"chat_1","model":"model","choices":[{"delta":{"role":"assistant","reasoning":"trace","reasoning_details":[{"type":"reasoning.text","text":"trace"}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"chat_1","model":"model","choices":[{"delta":{"content":"answer"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chat_1","model":"model","choices":[{"delta":{"reasoning":null},"finish_reason":"stop"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	decoded, err := backend.Codec.Decode(context.Background(), provider.Request{ExchangeID: "ex", Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model")})}, provider.StreamIngress{Stream: carrier.ByteStream{
		MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw)),
	}})
	if err != nil {
		t.Fatalf("decode terminal null reasoning: %v", err)
	}
	events := drainEvents(t, decoded.Stream)
	if len(events) == 0 {
		t.Fatal("no events decoded")
	}
}

func openRouterReasoningItem(t *testing.T, summary string) canonical.CanonicalItem {
	t.Helper()
	raw, err := json.Marshal([]map[string]string{{"type": "reasoning.summary", "summary": summary}})
	if err != nil {
		t.Fatal(err)
	}
	opaque, err := canonical.NewProviderChatOpaqueThinking(ChatReplayScope, raw)
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, summary)
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func openRouterBackend(t *testing.T, model string) provider.Backend {
	return openRouterBackendForProtocol(t, model, protocolkind.ChatCompletions)
}

func openRouterBackendForProtocol(t *testing.T, model string, protocol protocolkind.ProtocolKind) provider.Backend {
	t.Helper()
	target := provider.NewTargetSnapshot("openrouter", string(profile.ProviderSpecOpenRouter), "https://openrouter.test/api/v1", "env:OPENROUTER_API_KEY", protocol, string(protocol), delivery.BufferedDelivery())
	target.Model = model
	backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func messageItem(t *testing.T, role canonical.MessageRole, text string) canonical.CanonicalItem {
	t.Helper()
	item, err := canonical.NewMessageItem(role, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func drainEvents(t *testing.T, stream canonical.ResponseStream) []canonical.Event {
	t.Helper()
	defer stream.Close(context.Background())
	var events []canonical.Event
	for {
		event, err := stream.Next(context.Background())
		if err == io.EOF {
			return events
		}
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
}
