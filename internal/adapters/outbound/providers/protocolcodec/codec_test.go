package protocolcodec

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
	"github.com/swobuforge/swobu/internal/wire/responses"
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

func TestParallelToolCallsFalseProjectionUsesAttemptFactAtEmission(t *testing.T) {
	tool := canonicaltest.MustFunctionTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search"),
		"Search", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool](),
	)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, tool),
			canonicaltest.Message(t, canonical.MessageRoleUser, "search"),
		},
		ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}

	for _, protocol := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses} {
		t.Run(string(protocol), func(t *testing.T) {
			encode := func(known bool, value bool) (map[string]any, []compat.Change, map[provider.TargetFact]bool) {
				facts := provider.NewTargetFacts(func(fact provider.TargetFact) (bool, bool) {
					if fact != provider.AcceptsParallelToolCallsFalse {
						t.Fatalf("unexpected fact read: %v", fact)
					}
					return value, known
				})
				document, changes, encodeErr := (Codec{Protocol: protocol}).Encode(provider.Request{
					Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery(), TargetFacts: facts,
				})
				if encodeErr != nil {
					t.Fatal(encodeErr)
				}
				var payload map[string]any
				if unmarshalErr := json.Unmarshal(document.RawBytes(), &payload); unmarshalErr != nil {
					t.Fatal(unmarshalErr)
				}
				return payload, changes, facts.Reads()
			}

			unknown, unknownChanges, unknownReads := encode(false, false)
			accepted, acceptedChanges, acceptedReads := encode(true, true)
			if !bytes.Equal(mustJSON(t, unknown), mustJSON(t, accepted)) {
				t.Fatalf("unknown projection = %#v, known-true projection = %#v", unknown, accepted)
			}
			if unknown["parallel_tool_calls"] != false || len(unknownChanges) != 0 || len(acceptedChanges) != 0 {
				t.Fatalf("preferred projection=%#v unknown changes=%#v accepted changes=%#v", unknown, unknownChanges, acceptedChanges)
			}
			if !unknownReads[provider.AcceptsParallelToolCallsFalse] || !acceptedReads[provider.AcceptsParallelToolCallsFalse] {
				t.Fatalf("preferred reads: unknown=%#v accepted=%#v", unknownReads, acceptedReads)
			}

			fallback, fallbackChanges, fallbackReads := encode(true, false)
			if _, exists := fallback["parallel_tool_calls"]; exists {
				t.Fatalf("fallback projection retained parallel_tool_calls: %#v", fallback)
			}
			wantChange := compat.NewOmission(canonical.RequestToolCallBatch, canonical.Occurrence{})
			if len(fallbackChanges) != 1 || fallbackChanges[0] != wantChange {
				t.Fatalf("fallback changes = %#v, want %#v", fallbackChanges, wantChange)
			}
			if len(fallbackReads) != 1 || fallbackReads[provider.AcceptsParallelToolCallsFalse] {
				t.Fatalf("fallback reads = %#v", fallbackReads)
			}
		})
	}
}

func TestMaxCompletionTokensProjectionUsesFactOnlyForPreferredCarrier(t *testing.T) {
	maxTokens := 64
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{MaxOutputTokens: &maxTokens})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    canonical.Specify("model"),
		Items:    []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		Controls: controls,
	})

	t.Run("known false falls back exactly", func(t *testing.T) {
		facts := provider.NewTargetFacts(func(fact provider.TargetFact) (bool, bool) {
			if fact != provider.AcceptsMaxCompletionTokens {
				t.Fatalf("unexpected fact read: %v", fact)
			}
			return false, true
		})
		document, changes, encodeErr := (Codec{
			Protocol:    protocolkind.ChatCompletions,
			ChatDialect: ChatDialect{UseMaxCompletionTokens: true},
		}).Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery(), TargetFacts: facts})
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		var payload map[string]any
		if unmarshalErr := json.Unmarshal(document.RawBytes(), &payload); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		if _, exists := payload["max_completion_tokens"]; exists || payload["max_tokens"] != float64(maxTokens) {
			t.Fatalf("fallback projection = %#v", payload)
		}
		if len(changes) != 0 || len(facts.Reads()) != 1 || facts.Reads()[provider.AcceptsMaxCompletionTokens] {
			t.Fatalf("changes=%#v reads=%#v", changes, facts.Reads())
		}
	})

	t.Run("static max tokens grammar does not read fact", func(t *testing.T) {
		facts := provider.NewTargetFacts(func(fact provider.TargetFact) (bool, bool) {
			t.Fatalf("static grammar read fact: %v", fact)
			return false, false
		})
		document, changes, encodeErr := (Codec{Protocol: protocolkind.ChatCompletions}).Encode(provider.Request{
			Canonical: request, Delivery: delivery.BufferedDelivery(), TargetFacts: facts,
		})
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if !bytes.Contains(document.RawBytes(), []byte(`"max_tokens":64`)) || len(changes) != 0 || len(facts.Reads()) != 0 {
			t.Fatalf("document=%s changes=%#v reads=%#v", document.RawBytes(), changes, facts.Reads())
		}
	})
}

func TestOrdinalReasoningFactsSelectOnlyExecutedProjection(t *testing.T) {
	encode := func(t *testing.T, protocol protocolkind.ProtocolKind, request canonical.CanonicalRequest, rejected provider.TargetFact) (map[string]any, []compat.Change, map[provider.TargetFact]bool) {
		t.Helper()
		facts := provider.NewTargetFacts(func(fact provider.TargetFact) (bool, bool) {
			if fact != rejected {
				t.Fatalf("unexpected fact read: %v", fact)
			}
			return false, true
		})
		document, changes, err := (Codec{Protocol: protocol}).Encode(provider.Request{
			Canonical: request, Delivery: delivery.BufferedDelivery(), TargetFacts: facts,
		})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload, changes, facts.Reads()
	}

	max := canonical.InferenceEffortMax
	maxControls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &max})
	if err != nil {
		t.Fatal(err)
	}
	maxRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Controls: maxControls,
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})
	disabledControls, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: canonical.Specify(canonical.NewDisabledReasoningCompute()),
	})
	if err != nil {
		t.Fatal(err)
	}
	disabledRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Reasoning: disabledControls,
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})

	for _, protocol := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses} {
		t.Run(string(protocol)+" max", func(t *testing.T) {
			payload, changes, reads := encode(t, protocol, maxRequest, provider.AcceptsReasoningEffortMax)
			got := payload["reasoning_effort"]
			if protocol == protocolkind.Responses {
				reasoning, _ := payload["reasoning"].(map[string]any)
				got = reasoning["effort"]
			}
			want := compat.NewApproximation(canonical.RequestControlsEffort, canonical.Occurrence{})
			if got != "xhigh" || len(changes) != 1 || changes[0] != want || len(reads) != 1 || reads[provider.AcceptsReasoningEffortMax] {
				t.Fatalf("payload=%#v changes=%#v reads=%#v", payload, changes, reads)
			}
		})
		t.Run(string(protocol)+" disabled", func(t *testing.T) {
			payload, changes, reads := encode(t, protocol, disabledRequest, provider.AcceptsReasoningDisabled)
			if _, present := payload["reasoning_effort"]; present {
				t.Fatalf("disabled carrier retained: %#v", payload)
			}
			if reasoning, present := payload["reasoning"].(map[string]any); present {
				if _, effortPresent := reasoning["effort"]; effortPresent {
					t.Fatalf("disabled effort retained: %#v", payload)
				}
			}
			want := compat.NewOmission(canonical.RequestReasoning, canonical.Occurrence{})
			if len(changes) != 1 || changes[0] != want || len(reads) != 1 || reads[provider.AcceptsReasoningDisabled] {
				t.Fatalf("payload=%#v changes=%#v reads=%#v", payload, changes, reads)
			}
		})
	}
}

func TestResponsesFunctionOutputArrayFactSelectsExactStringOrImageRehome(t *testing.T) {
	encode := func(t *testing.T, parts []canonical.ToolResultPart) (map[string]any, []compat.Change, map[provider.TargetFact]bool) {
		t.Helper()
		callID, _ := canonical.NewToolCallID("call_result")
		key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "result_tool")
		declarations := canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]()))
		call := canonicaltest.ToolCall(t, callID.String(), key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
		result, err := canonical.NewToolResultItem(callID, parts, false)
		if err != nil {
			t.Fatal(err)
		}
		facts := provider.NewTargetFacts(func(fact provider.TargetFact) (bool, bool) {
			if fact != provider.AcceptsFunctionCallOutputArray {
				t.Fatalf("unexpected fact read: %v", fact)
			}
			return false, true
		})
		request := canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{declarations, call, result},
		})
		names, _, err := provider.BuildAttemptToolNames(request)
		if err != nil {
			t.Fatal(err)
		}
		document, changes, err := (Codec{Protocol: protocolkind.Responses}).Encode(provider.Request{
			Canonical: request, Delivery: delivery.BufferedDelivery(), TargetFacts: facts, ToolNames: names,
		})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload, changes, facts.Reads()
	}

	t.Run("text is exact string", func(t *testing.T) {
		payload, changes, reads := encode(t, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")})
		input := payload["input"].([]any)
		output := input[1].(map[string]any)
		if output["call_id"] != "call_result" || output["output"] != "done" || len(changes) != 0 || len(reads) != 1 || reads[provider.AcceptsFunctionCallOutputArray] {
			t.Fatalf("payload=%#v changes=%#v reads=%#v", payload, changes, reads)
		}
	})

	t.Run("multimodal preserves correlation and model-visible image", func(t *testing.T) {
		image, err := canonical.NewURLImage("https://example.test/result.png", canonical.Unspecified[canonical.ImageDetail]())
		if err != nil {
			t.Fatal(err)
		}
		payload, changes, reads := encode(t, []canonical.ToolResultPart{
			canonical.NewTextToolResultPart("caption"), canonical.NewImageToolResultPart(image),
		})
		input := payload["input"].([]any)
		output := input[1].(map[string]any)
		message := input[2].(map[string]any)
		content := message["content"].([]any)
		wireImage := content[0].(map[string]any)
		want := compat.NewApproximation(canonical.RequestItemsToolResultImage, canonical.Occurrence{})
		if output["call_id"] != "call_result" || output["output"] != "caption" ||
			message["role"] != "user" || wireImage["image_url"] != "https://example.test/result.png" ||
			len(changes) != 1 || changes[0] != want || len(reads) != 1 || reads[provider.AcceptsFunctionCallOutputArray] {
			t.Fatalf("payload=%#v changes=%#v reads=%#v", payload, changes, reads)
		}
	})
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestChatCompletionsWebSearchOmitsWithoutNativeLowering(t *testing.T) {
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, set.Declarations()...),
			canonicaltest.Message(t, canonical.MessageRoleUser, "search"),
		},
	})
	document, changes, err := (Codec{Protocol: protocolkind.ChatCompletions}).Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil || len(document.RawBytes()) == 0 || len(changes) != 1 || changes[0].Kind != compat.Omission {
		t.Fatalf("document=%s changes=%#v err=%v", document.RawBytes(), changes, err)
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
		var swobuErr canonical.Error
		if !errors.As(encodeErr, &swobuErr) || swobuErr.Code != canonical.ErrorCodeInternal {
			t.Fatalf("%s encode error = %T %v, want INTERNAL_ERROR", protocol, encodeErr, encodeErr)
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
		var swobuErr canonical.Error
		if !errors.As(err, &swobuErr) || swobuErr.Code != canonical.ErrorCodeInternal {
			t.Fatalf("policy %s error = %T %v, want INTERNAL_ERROR", policy.Mode, err, err)
		}
	}
}

func TestChatCompletionsCodecOmitsWebSearchWithoutNativeLowering(t *testing.T) {
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
	document, changes, err := CompileChatRequest(request, ChatDialect{})
	if err != nil || len(changes) != 1 || changes[0].Kind != compat.Omission {
		t.Fatalf("document=%#v changes=%#v err=%v", document, changes, err)
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

func TestCompileChatRequest_ToolPolicyConsumesEmittedProjection(t *testing.T) {
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

	dialect := ChatDialect{
		Lowering: chatcompletions.Lowering{Tools: chatcompletions.ToolLowering{WebSearch: func(_ chatcompletions.ToolLoweringContext, _ canonical.ToolDeclaration) (chatcompletions.ToolProjection, []compat.Change, error) {
			return chatcompletions.ToolProjection{Fragments: []chatcompletions.ToolFragment{{Value: map[string]any{"type": "browser_search"}}}, TargetType: "browser_search"}, nil, nil
		}}},
	}
	doc, _, err := CompileChatRequest(request, dialect)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
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

func TestResponsesCustomAsFunctionUsesOneEmittedCallableForDeclarationHistoryAndPolicy(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "shell")
	tool := canonicaltest.MustCustomTool(key, "Run shell text", canonical.EmptyToolFormat())
	declarations := canonicaltest.ToolDeclarations(t, tool)
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewTextToolInput("echo exact"))
	callValue, _ := call.ToolCall()
	result, err := canonical.NewToolResultItem(
		callValue.CallID(),
		[]canonical.ToolResultPart{canonical.NewTextToolResultPart("done")},
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	policy := canonical.NewToolPolicy(canonical.ToolPolicySpecific, &key)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{declarations, call, result}, ToolPolicy: canonical.Specify(policy),
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, changes, err := (Codec{
		Protocol:         protocolkind.Responses,
		ResponsesDialect: ResponsesDialect{Tools: responses.ToolLowering{Custom: ResponsesCustomAsFunction()}},
	}).Encode(provider.Request{
		Canonical: request, Delivery: delivery.BufferedDelivery(), ToolNames: names,
		TargetFacts: provider.NewTargetFacts(func(fact provider.TargetFact) (bool, bool) {
			if fact == provider.AcceptsFunctionCallOutputArray {
				return false, true
			}
			return true, false
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("plaintext wrapper changes = %#v, want exact", changes)
	}
	wire := string(document.RawBytes())
	for _, want := range []string{
		`"tools":[{"type":"function","name":"shell"`,
		`"parameters":{"additionalProperties":false,"properties":{"input":{"type":"string"}},"required":["input"],"type":"object"}`,
		`"tool_choice":{"name":"shell","type":"function"}`,
		`"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"input\":\"echo exact\"}"`,
		`"type":"function_call_output","call_id":"call_1","output":"done"`,
	} {
		if !strings.Contains(wire, want) {
			t.Fatalf("wrapped custom request = %s, want %s", wire, want)
		}
	}
	if strings.Contains(wire, "custom_tool_call") || strings.Contains(wire, `"type":"custom"`) {
		t.Fatalf("wrapped custom request leaked native Custom syntax: %s", wire)
	}
}

func TestResponsesCustomSlotAloneOwnsAWeirdCompleteProjection(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "canonical-shell")
	tool := canonicaltest.MustCustomTool(key, "Run raw text", canonical.EmptyToolFormat())
	call := canonicaltest.ToolCall(t, "call_weird", key, canonical.NewTextToolInput("abc"))
	callValue, _ := call.ToolCall()
	result, err := canonical.NewToolResultItem(callValue.CallID(), []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify("model"),
		Items:      []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tool), call, result},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &key)),
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	weirdCustom := func(_ responses.ToolLoweringContext, _ canonical.ToolDeclaration) (responses.ToolProjection, []compat.Change, error) {
		encoded := responses.ProviderRequestTool{
			Type: "function", Name: "x", Parameters: map[string]any{
				"type": "object", "properties": map[string]any{"payload": map[string]any{"type": "string"}}, "required": []string{"payload"},
			},
		}
		return responses.CustomAsFunctionProjection(encoded, "payload"), nil, nil
	}
	document, _, err := (Codec{
		Protocol:         protocolkind.Responses,
		ResponsesDialect: ResponsesDialect{Tools: responses.ToolLowering{Custom: weirdCustom}},
	}).Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery(), ToolNames: names})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(document.RawBytes())
	for _, want := range []string{
		`"tools":[{"type":"function","name":"x"`,
		`"tool_choice":{"name":"x","type":"function"}`,
		`"type":"function_call","call_id":"call_weird","name":"x","arguments":"{\"payload\":\"abc\"}"`,
		`"type":"function_call_output","call_id":"call_weird"`,
	} {
		if !strings.Contains(wire, want) {
			t.Fatalf("weird Custom projection = %s, want %s", wire, want)
		}
	}
}
