package protocolcodec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/testkit/providertest"
	"github.com/swobuforge/swobu/internal/wire"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

func TestStandardProtocolCacheSensitiveRenderingIsDeterministic(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "one"),
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "two"),
		},
	})
	for _, protocol := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses, protocolkind.Messages} {
		t.Run(string(protocol), func(t *testing.T) {
			codec := Codec{Protocol: protocol}
			projection := func(exchangeID string, deliveryMode delivery.Delivery) []byte {
				document, _, err := codec.Encode(provider.Request{ExchangeID: exchangeID, Canonical: request, Delivery: deliveryMode})
				if err != nil {
					t.Fatal(err)
				}
				value, err := providertest.CacheSensitiveProjection(document)
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
			first := projection("exchange-a", delivery.BufferedDelivery())
			second := projection("exchange-b", delivery.BufferedDelivery())
			streamed := projection("exchange-c", delivery.StreamingDelivery(delivery.FramingSSE))
			if !bytes.Equal(first, second) || !bytes.Equal(first, streamed) {
				t.Fatalf("cache-sensitive projection changed: buffered=%s repeated=%s streamed=%s", first, second, streamed)
			}
			appended := request.WithItems(append(request.Items(), canonicaltest.Message(t, canonical.MessageRoleUser, "three")))
			document, _, err := codec.Encode(provider.Request{Canonical: appended, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			appendProjection, err := providertest.CacheSensitiveProjection(document)
			if err != nil || bytes.Equal(first, appendProjection) || !bytes.Contains(appendProjection, []byte("one")) || !bytes.Contains(appendProjection, []byte("two")) {
				t.Fatalf("append projection = %s (%v)", appendProjection, err)
			}
		})
	}
}

func TestEncodeSelectsFullChatHistoryAndNativeResponsesDelta(t *testing.T) {
	full := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "first turn"),
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "first answer"),
			canonicaltest.Message(t, canonical.MessageRoleUser, "second turn"),
		},
	})
	chat, _, err := (Codec{Protocol: protocolkind.ChatCompletions}).Encode(provider.Request{Canonical: full, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if raw := string(chat.RawBytes()); !strings.Contains(raw, "first turn") || !strings.Contains(raw, "first answer") || !strings.Contains(raw, "second turn") {
		t.Fatalf("chat codec did not preserve full history: %s", raw)
	}

	responses, _, err := (Codec{Protocol: protocolkind.Responses, ResponsesDialect: ResponsesDialect{CaptureResponsesContinuation: true}}).Encode(provider.Request{
		Canonical: full, Delivery: delivery.BufferedDelivery(),
		PreviousHistory: &provider.PreviousHistory{Response: canonical.ResponseRef{Responses: &canonical.ResponsesContinuation{ProviderResponseID: "provider_previous", TargetID: "target", TargetVersion: 1}}, OmitStart: 0, OmitEnd: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if raw := string(responses.RawBytes()); !strings.Contains(raw, `"previous_response_id":"provider_previous"`) || strings.Contains(raw, "first turn") {
		t.Fatalf("responses codec did not preserve native delta selection: %s", raw)
	}
}

func TestResponsesCodecUsesSharedOfficialToolLowering(t *testing.T) {
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
	document, changes, err := (Codec{Protocol: protocolkind.Responses}).Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	raw := string(document.RawBytes())
	wireName, _ := names.WireName(childKey)
	if strings.Contains(raw, `"type":"namespace"`) || !strings.Contains(raw, `"name":"`+wireName+`"`) {
		t.Fatalf("flat Responses document = %s", raw)
	}
	if len(changes) != 1 || changes[0].Capability != canonical.RequestTools {
		t.Fatalf("flat Responses changes = %#v", changes)
	}
}

func TestChatCompletionsWebSearchUsesProtocolDefault(t *testing.T) {
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, set.Declarations()...),
			canonicaltest.Message(t, canonical.MessageRoleUser, "search"),
		},
	})
	_, _, err := (Codec{Protocol: protocolkind.ChatCompletions}).Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("generic hosted search error = %T %v, want typed incompatibility", err, err)
	}
}

func TestProviderCodecsRejectResidualMCPInsteadOfChangingExecutionOwner(t *testing.T) {
	key, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "mail")
	source, _ := canonical.NewMCPConnectorSource(
		"connector_gmail", canonical.Specify([]string{"search", "send"}),
		canonical.NewMCPApprovalNever(), canonical.MCPLoadingDeferred,
		canonical.Specify([]string{"direct"}),
	)
	declaration, _ := canonical.NewMCPToolSource(key, "Mail", source, nil)
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			item, canonicaltest.Message(t, canonical.MessageRoleUser, "search mail"),
		},
	})
	providerRequest := provider.Request{
		Canonical: request, Delivery: delivery.BufferedDelivery(),
	}

	for _, protocol := range []protocolkind.ProtocolKind{
		protocolkind.Responses, protocolkind.ChatCompletions, protocolkind.Messages,
	} {
		_, _, encodeErr := (Codec{Protocol: protocol}).Encode(providerRequest)
		var incompatible provider.IncompatibleTargetError
		if !errors.As(encodeErr, &incompatible) {
			t.Fatalf("%s encode error = %T %v, want target incompatibility", protocol, encodeErr, encodeErr)
		}
	}
}

func TestFlatResponsesFailsWhenPolicyDependsOnResidualMCP(t *testing.T) {
	key, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "mail")
	filter, _ := canonical.NewMCPToolFilter(
		canonical.Specify([]string{"send"}), canonical.Unspecified[bool](),
	)
	approval, _ := canonical.NewMCPApprovalFilter(&filter, nil)
	source, _ := canonical.NewMCPConnectorSource(
		"connector_gmail", canonical.Unspecified[[]string](), approval,
		canonical.MCPLoadingEager, canonical.Unspecified[[]string](),
	)
	declaration, _ := canonical.NewMCPToolSource(key, "", source, nil)
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	for _, policy := range []canonical.ToolPolicy{
		canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
		canonical.NewToolPolicy(canonical.ToolPolicySpecific, &key),
	} {
		request := canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("model"), ToolPolicy: canonical.Specify(policy),
			Items: []canonical.CanonicalItem{item, canonicaltest.Message(t, canonical.MessageRoleUser, "send")},
		})
		_, _, err := (Codec{Protocol: protocolkind.Responses}).Encode(provider.Request{
			Canonical: request, Delivery: delivery.BufferedDelivery(),
		})
		var incompatible provider.IncompatibleTargetError
		if !errors.As(err, &incompatible) {
			t.Fatalf("policy %s error = %T %v, want target incompatibility", policy.Mode, err, err)
		}
	}
}

func TestChatCompletionsCodecSerializesSharedTypedLowering(t *testing.T) {
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	request := provider.Request{
		ExchangeID: "exchange-shared-lowering",
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("model"),
			Items: []canonical.CanonicalItem{
				canonicaltest.ToolDeclarations(t, set.Declarations()...),
				canonicaltest.Message(t, canonical.MessageRoleUser, "search"),
			},
		}),
		Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
	}
	_, _, err := CompileChatRequest(request, ChatDialect{})
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("generic hosted search error = %T %v, want typed incompatibility", err, err)
	}
}

func TestDecodePreservesExchangeIdentityAndCancellation(t *testing.T) {
	codec := Codec{Protocol: protocolkind.ChatCompletions}
	request := provider.Request{ExchangeID: "exchange-identity", Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model")})}
	decoded, err := codec.Decode(context.Background(), request, provider.DocumentIngress{Document: carrier.NewDocument(
		protocolkind.ChatCompletions, "application/json", nil,
		[]byte(`{"id":"provider-response","model":"model","choices":[{"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}]}`), carrier.Meta{},
	)})
	if err != nil {
		t.Fatal(err)
	}
	event, err := decoded.Stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.ExchangeID != request.ExchangeID {
		t.Fatalf("exchange id = %q", event.ExchangeID)
	}

	body := &blockingReadCloser{closed: make(chan struct{})}
	streamed, err := codec.Decode(context.Background(), request, provider.StreamIngress{Stream: carrier.ByteStream{
		Header: http.Header{"Content-Type": {"text/event-stream"}}, MediaType: "text/event-stream", Body: body,
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, nextErr := streamed.Stream.Next(ctx)
		result <- nextErr
	}()
	cancel()
	select {
	case nextErr := <-result:
		if !errors.Is(nextErr, context.Canceled) {
			t.Fatalf("Next error = %v", nextErr)
		}
	case <-time.After(time.Second):
		t.Fatal("protocol codec stream ignored cancellation")
	}
}

func TestResponsesContinuationCaptureRequiresPersistenceEligibleRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		requestStore  canonical.Specified[bool]
		responseStore string
		streaming     bool
		wantCapture   bool
	}{
		{name: "buffered request omitted response true", requestStore: canonical.Unspecified[bool](), responseStore: `,"store":true`, wantCapture: true},
		{name: "buffered request true response true", requestStore: canonical.Specify(true), responseStore: `,"store":true`, wantCapture: true},
		{name: "buffered request false response true", requestStore: canonical.Specify(false), responseStore: `,"store":true`},
		{name: "buffered request omitted response false", requestStore: canonical.Unspecified[bool](), responseStore: `,"store":false`},
		{name: "buffered request true response false", requestStore: canonical.Specify(true), responseStore: `,"store":false`},
		{name: "buffered response store absent", requestStore: canonical.Unspecified[bool]()},
		{name: "streamed request omitted response true", requestStore: canonical.Unspecified[bool](), responseStore: `,"store":true`, streaming: true, wantCapture: true},
		{name: "streamed request true response true", requestStore: canonical.Specify(true), responseStore: `,"store":true`, streaming: true, wantCapture: true},
		{name: "streamed request false response true", requestStore: canonical.Specify(false), responseStore: `,"store":true`, streaming: true},
		{name: "streamed request omitted response false", requestStore: canonical.Unspecified[bool](), responseStore: `,"store":false`, streaming: true},
		{name: "streamed request true response false", requestStore: canonical.Specify(true), responseStore: `,"store":false`, streaming: true},
		{name: "streamed response store absent", requestStore: canonical.Unspecified[bool](), streaming: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := provider.Request{
				ExchangeID: "exchange-store-policy",
				Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
					Model: canonical.Specify("model"),
					Store: test.requestStore,
				}),
			}
			codec := Codec{Protocol: protocolkind.Responses, ResponsesDialect: ResponsesDialect{CaptureResponsesContinuation: true}}
			buffered := `{"id":"resp_store","model":"model","status":"completed","output":[]` + test.responseStore + `}`
			var ingress provider.Ingress = provider.DocumentIngress{Document: carrier.NewDocument(
				protocolkind.Responses,
				"application/json",
				nil,
				[]byte(buffered),
				carrier.Meta{},
			)}
			if test.streaming {
				ingress = provider.StreamIngress{Stream: carrier.ByteStream{
					Header:    http.Header{"Content-Type": {"text/event-stream"}},
					MediaType: "text/event-stream",
					Body: io.NopCloser(strings.NewReader(
						"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_store\",\"model\":\"model\",\"status\":\"in_progress\"" + test.responseStore + "}}\n\n" +
							"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_store\",\"model\":\"model\",\"status\":\"completed\",\"output\":[]}}\n\n",
					)),
				}}
			}
			decoded, err := codec.Decode(context.Background(), request, ingress)
			if err != nil {
				t.Fatal(err)
			}
			captured := responsesContinuationCaptured(t, decoded.Stream)
			if captured != test.wantCapture {
				t.Fatalf("continuation captured = %t, want %t", captured, test.wantCapture)
			}
		})
	}
}

func responsesContinuationCaptured(t *testing.T, stream canonical.ResponseStream) bool {
	t.Helper()
	defer func() {
		if err := stream.Close(context.Background()); err != nil {
			t.Fatalf("close response stream: %v", err)
		}
	}()
	for {
		event, err := stream.Next(context.Background())
		if errors.Is(err, io.EOF) {
			return false
		}
		if err != nil {
			t.Fatalf("read response stream: %v", err)
		}
		if event.Kind != canonical.EventResponseIdentity {
			continue
		}
		identity, ok := event.Payload.(canonical.ResponseIdentityPayload)
		if !ok {
			t.Fatalf("response identity payload = %T", event.Payload)
		}
		return identity.Response.Responses != nil
	}
}

type blockingReadCloser struct{ closed chan struct{} }

func (b *blockingReadCloser) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *blockingReadCloser) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

func TestCompileChatRequest_ToolPolicyLoweringRule(t *testing.T) {
	webSearch := canonical.NewWebSearchDeclaration()
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{webSearch})
	searchKey := webSearch.Key()
	request := provider.Request{
		ExchangeID: "exchange-policy-lowering",
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:      canonical.Specify("model"),
			ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &searchKey)),
			Items: []canonical.CanonicalItem{
				canonicaltest.ToolDeclarations(t, set.Declarations()...),
				canonicaltest.Message(t, canonical.MessageRoleUser, "search"),
			},
		}),
		Delivery: delivery.BufferedDelivery(),
	}

	// 1. Without LowerToolPolicy, specific tool choice on semantic tool must fail with IncompatibleTargetError
	dialectWithoutPolicy := ChatDialect{
		LowerTool: func(_ chatcompletions.ToolLoweringContext, tool canonical.ToolDeclaration) ([]any, bool, []compat.Change, error) {
			if tool.Kind() == canonical.ToolKindWebSearch {
				return []any{map[string]any{"type": "browser_search"}}, true, nil, nil
			}
			return nil, false, nil, nil
		},
	}
	_, _, err := CompileChatRequest(request, dialectWithoutPolicy)
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("without LowerToolPolicy error = %T %v, want IncompatibleTargetError", err, err)
	}

	// 2. With LowerToolPolicy, provider produces target-aware tool choice
	dialectWithPolicy := ChatDialect{
		LowerTool: dialectWithoutPolicy.LowerTool,
		LowerToolPolicy: func(policy canonical.ToolPolicy, lowered wire.LoweredToolSet, names wire.ToolNames) (any, bool, []compat.Change, error) {
			if policy.Mode == canonical.ToolPolicySpecific {
				return map[string]any{"type": "browser_search"}, true, nil, nil
			}
			return nil, false, nil, nil
		},
	}
	doc, _, err := CompileChatRequest(request, dialectWithPolicy)
	if err != nil {
		t.Fatalf("with LowerToolPolicy failed: %v", err)
	}
	encoded, err := chatcompletions.EncodeProviderRequestDocument(doc)
	if err != nil {
		t.Fatalf("encode document failed: %v", err)
	}
	raw := string(encoded.RawBytes())
	if !strings.Contains(raw, `"tool_choice":{"type":"browser_search"}`) {
		t.Fatalf("wire document missing lowered tool_choice: %s", raw)
	}
}

func TestCodecAttemptDecoration_RejectsSemanticCollisions(t *testing.T) {
	request := provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("model"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		}),
		Delivery: delivery.BufferedDelivery(),
	}

	chatReserved := []string{
		"model", "messages", "tools", "tool_choice", "parallel_tool_calls",
		"functions", "function_call", "stream", "stream_options",
		"temperature", "top_p", "max_tokens", "max_completion_tokens",
		"stop", "response_format", "reasoning_effort", "reasoning",
		"thinking", "include_reasoning", "n", "presence_penalty",
		"frequency_penalty", "seed", "user", "logprobs", "top_logprobs",
		"logit_bias", "modalities",
	}

	for _, field := range chatReserved {
		t.Run("Chat_"+field, func(t *testing.T) {
			codec := Codec{
				Protocol: protocolkind.ChatCompletions,
				ChatDialect: ChatDialect{
					DecorateAttempt: func(_ AttemptContext) (AttemptDecoration, error) {
						return AttemptDecoration{Fields: map[string]any{field: "override"}}, nil
					},
				},
			}
			if _, _, err := codec.Encode(request); err == nil {
				t.Fatalf("expected error when Chat DecorateAttempt mutates reserved semantic field %q", field)
			}
		})
	}

	responsesReserved := []string{
		"model", "input", "instructions", "tools", "tool_choice",
		"parallel_tool_calls", "stream", "temperature", "top_p",
		"max_output_tokens", "stop", "response_format", "text",
		"output_format", "include", "store", "previous_response_id",
		"reasoning_effort", "reasoning", "conversation", "metadata", "user",
	}

	for _, field := range responsesReserved {
		t.Run("Responses_"+field, func(t *testing.T) {
			codec := Codec{
				Protocol: protocolkind.Responses,
				ResponsesDialect: ResponsesDialect{
					DecorateAttempt: func(_ AttemptContext) (AttemptDecoration, error) {
						return AttemptDecoration{Fields: map[string]any{field: "override"}}, nil
					},
				},
			}
			if _, _, err := codec.Encode(request); err == nil {
				t.Fatalf("expected error when Responses DecorateAttempt mutates reserved semantic field %q", field)
			}
		})
	}

	messagesReserved := []string{
		"model", "messages", "system", "tools", "tool_choice",
		"stream", "temperature", "top_p", "top_k", "max_tokens",
		"stop_sequences", "thinking", "output_config", "metadata",
	}

	for _, field := range messagesReserved {
		t.Run("Messages_"+field, func(t *testing.T) {
			codec := Codec{
				Protocol: protocolkind.Messages,
				MessagesDialect: MessagesDialect{
					DecorateAttempt: func(_ AttemptContext) (AttemptDecoration, error) {
						return AttemptDecoration{Fields: map[string]any{field: "override"}}, nil
					},
				},
			}
			if _, _, err := codec.Encode(request); err == nil {
				t.Fatalf("expected error when Messages DecorateAttempt mutates reserved semantic field %q", field)
			}
		})
	}
}

