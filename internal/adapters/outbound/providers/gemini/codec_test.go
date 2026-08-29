package gemini

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestCodecEncodesNativeTextHistoryAndPortableStoreIntent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		store      canonical.Specified[bool]
		wantStore  bool
		storeFound bool
	}{
		{name: "omitted", store: canonical.Unspecified[bool]()},
		{name: "true", store: canonical.Specify(true), wantStore: true, storeFound: true},
		{name: "false", store: canonical.Specify(false), storeFound: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("gemini-model-exact"),
				Store: tc.store,
				Items: []canonical.CanonicalItem{
					canonicaltest.Message(t, canonical.MessageRoleSystem, "Be concise."),
					canonicaltest.Message(t, canonical.MessageRoleUser, "first question"),
					canonicaltest.Message(t, canonical.MessageRoleAssistant, "first answer"),
					canonicaltest.Message(t, canonical.MessageRoleUser, "second question"),
				},
			})
			document, changes, err := (codec{}).Encode(provider.Request{
				Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(changes) != 0 {
				t.Fatalf("changes = %#v, want exact lowering", changes)
			}
			var payload struct {
				Model             string                 `json:"model"`
				Input             []interactionInputStep `json:"input"`
				SystemInstruction string                 `json:"system_instruction"`
				Store             *bool                  `json:"store"`
				Stream            bool                   `json:"stream"`
			}
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Model != "gemini-model-exact" || !payload.Stream || payload.SystemInstruction != "Be concise." {
				t.Fatalf("payload = %#v", payload)
			}
			if (payload.Store != nil) != tc.storeFound || payload.Store != nil && *payload.Store != tc.wantStore {
				t.Fatalf("store = %#v, want present=%t value=%t", payload.Store, tc.storeFound, tc.wantStore)
			}
			if len(payload.Input) != 3 {
				t.Fatalf("input = %#v, want three exact history steps", payload.Input)
			}
			for index, want := range []struct {
				typ, text string
			}{
				{typ: "user_input", text: "first question"},
				{typ: "model_output", text: "first answer"},
				{typ: "user_input", text: "second question"},
			} {
				got := payload.Input[index]
				if got.Type != want.typ || len(got.Content) != 1 || got.Content[0] != (interactionTextContent{Type: "text", Text: want.text}) {
					t.Fatalf("input[%d] = %#v, want %s/%q", index, got, want.typ, want.text)
				}
			}
		})
	}
}

func TestCodecStatelesslyReplaysInteractionsThoughtFunctionCallAndResult(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	callID, _ := canonical.NewToolCallID("call_lookup")
	opaque, err := canonical.NewInteractionsOpaqueThinking([]byte(`{"type":"thought","summary":[{"type":"text","text":"checking"}],"signature":"thought-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	part, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "checking")
	reasoning, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	call := canonicaltest.ToolCall(t, callID.String(), key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"x"}`)))
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("found")}, false)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gemini-model"), Store: canonical.Specify(false), Items: []canonical.CanonicalItem{
		canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())),
		canonicaltest.Message(t, canonical.MessageRoleUser, "look it up"), reasoning, call, result,
	}})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, changes, err := (codec{}).Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
	wire := string(document.RawBytes())
	for _, want := range []string{`"store":false`, `"type":"thought"`, `"signature":"thought-secret"`, `"type":"function_call"`, `"id":"call_lookup"`, `"arguments":{"q":"x"}`, `"type":"function_result"`, `"name":"lookup"`, `"call_id":"call_lookup"`} {
		if !strings.Contains(wire, want) {
			t.Fatalf("wire missing %s: %s", want, wire)
		}
	}
	if strings.Contains(wire, "previous_interaction_id") {
		t.Fatalf("stateless wire used native continuation: %s", wire)
	}
}

func TestCodecOmitsHistoricalCustomEffectAtomically(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "shell")
	callID, _ := canonical.NewToolCallID("call_shell")
	call := canonicaltest.ToolCall(t, callID.String(), key, canonical.NewTextToolInput("pwd"))
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("/workspace")}, false)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{call, result},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, changes, err := (codec{}).Encode(provider.Request{
		Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantChange := compat.NewOmission(canonical.RequestItemsKind, canonical.RequestItemOccurrence(0))
	if len(changes) != 1 || changes[0] != wantChange {
		t.Fatalf("changes = %#v, want %#v", changes, wantChange)
	}
	wire := string(document.RawBytes())
	if strings.Contains(wire, "call_shell") || strings.Contains(wire, "function_result") {
		t.Fatalf("wire retained half of omitted custom effect: %s", wire)
	}
}

func TestCodecReplaysSignatureOnlyThoughtAsExactRawStep(t *testing.T) {
	raw := []byte(`{"type":"thought","signature":"thought-secret","provider_private":{"keep":true}}`)
	opaque, err := canonical.NewInteractionsOpaqueThinking(raw)
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem(nil, opaque)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Store: canonical.Specify(false),
		Items: []canonical.CanonicalItem{reasoning},
	})
	document, _, err := (codec{}).Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Input) != 1 || !jsonEqual(payload.Input[0], raw) {
		t.Fatalf("input = %s, want exact opaque step %s", payload.Input, raw)
	}
	var step map[string]json.RawMessage
	if err := json.Unmarshal(payload.Input[0], &step); err != nil {
		t.Fatal(err)
	}
	if _, fabricated := step["summary"]; fabricated {
		t.Fatalf("signature-only replay fabricated summary: %s", payload.Input[0])
	}
}

func TestCodecRejectsMalformedOrMismatchedInteractionsReplayBeforeDispatch(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search_123")
	for name, raw := range map[string][]byte{
		"wrong call id":       []byte(`{"type":"google_search_call","id":"wrong_call","arguments":{"queries":["q"]},"search_type":"web_search","signature":"sig"}`),
		"missing signature":   []byte(`{"type":"google_search_call","id":"search_123","arguments":{"queries":["q"]},"search_type":"web_search"}`),
		"unsupported subtype": []byte(`{"type":"google_search_call","id":"search_123","arguments":{"queries":["q"]},"search_type":"image_search","signature":"sig"}`),
	} {
		t.Run(name, func(t *testing.T) {
			search, err := canonical.NewInteractionsWebSearchCall(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"q"}}, raw)
			if err != nil {
				t.Fatalf("canonical must retain opaque bytes without Gemini grammar validation: %v", err)
			}
			input, err := canonical.NewWebSearchToolInput(search)
			if err != nil {
				t.Fatal(err)
			}
			call := canonicaltest.ToolCall(t, callID.String(), canonical.WebSearchToolKey(), input)
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gemini-model"), Items: []canonical.CanonicalItem{call}})
			if _, _, err := (codec{}).Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}); err == nil {
				t.Fatal("Gemini accepted malformed or mismatched opaque Search replay")
			}
		})
	}
	for name, raw := range map[string][]byte{
		"wrong result call id":     []byte(`{"type":"google_search_result","call_id":"wrong_call","result":[],"signature":"sig"}`),
		"missing result signature": []byte(`{"type":"google_search_result","call_id":"search_123","result":[]}`),
	} {
		t.Run(name, func(t *testing.T) {
			search, err := canonical.NewWebSearchResult(nil)
			if err != nil {
				t.Fatal(err)
			}
			search, err = search.WithInteractionsReplay(raw)
			if err != nil {
				t.Fatalf("canonical must retain opaque bytes without Gemini grammar validation: %v", err)
			}
			result, err := canonical.NewWebSearchResultItem(callID, search)
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gemini-model"), Items: []canonical.CanonicalItem{result}})
			if _, _, err := (codec{}).Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}); err == nil {
				t.Fatal("Gemini accepted malformed or mismatched opaque Search result replay")
			}
		})
	}
}

func jsonEqual(left, right []byte) bool {
	var leftValue, rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil && reflect.DeepEqual(leftValue, rightValue)
}

func TestCodecMakesSingleSystemCarrierApproximationExplicit(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleSystem, "first directive"),
			canonicaltest.Message(t, canonical.MessageRoleDeveloper, "second directive"),
			canonicaltest.Message(t, canonical.MessageRoleUser, "question"),
			canonicaltest.Message(t, canonical.MessageRoleSystem, "late directive"),
		},
	})
	document, changes, err := (codec{}).Encode(provider.Request{
		Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload interactionRequest
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SystemInstruction != "first directive\n\nsecond directive\n\nlate directive" {
		t.Fatalf("system_instruction = %q", payload.SystemInstruction)
	}
	if len(changes) != 1 || changes[0].Capability != canonical.RequestInstructions || changes[0].Kind != compat.Approximation || !changes[0].Occurrence.IsZero() {
		t.Fatalf("changes = %#v, want one directive approximation", changes)
	}
}

func TestCodecRetainsRequiredEmptyInputArrayForDirectiveOnlyRequest(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleSystem, "Follow this instruction."),
		},
	})
	document, _, err := (codec{}).Encode(provider.Request{
		Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	rawInput, ok := payload["input"]
	if !ok || string(rawInput) != "[]" {
		t.Fatalf("input = %s, want required empty array", rawInput)
	}
}

func TestCodecLowersTypedGeminiPreviousHistoryAndRetainsCurrentRequestScope(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.MustInstruction(canonical.MessageRoleSystem, "current directive"),
			canonicaltest.Message(t, canonical.MessageRoleUser, "first question"),
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "first answer"),
			canonicaltest.Message(t, canonical.MessageRoleUser, "second question"),
		},
	})
	document, _, err := (codec{}).Encode(provider.Request{
		Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
		PreviousHistory: &provider.PreviousHistory{
			Response: canonical.ResponseRef{SwobuID: "resp_previous", Interactions: &canonical.InteractionsContinuation{
				ProviderInteractionID: canonical.NewInteractionID("interaction_previous"), TargetID: "gemini-target", TargetVersion: 1,
			}},
			OmitStart: 1, OmitEnd: 3,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if raw := string(document.RawBytes()); !strings.Contains(raw, `"previous_interaction_id":"interaction_previous"`) {
		t.Fatalf("native continuation missing from wire: %s", raw)
	}
	var payload interactionRequest
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PreviousInteractionID != "interaction_previous" || payload.SystemInstruction != "current directive" {
		t.Fatalf("payload = %#v, want native handle and current directive", payload)
	}
	if len(payload.Input) != 1 || payload.Input[0].Type != "user_input" || len(payload.Input[0].Content) != 1 || payload.Input[0].Content[0].Text != "second question" {
		t.Fatalf("input = %#v, want only current user turn", payload.Input)
	}
}

func TestCodecIgnoresForeignPreviousHistoryAndRejectsMalformedGeminiHistory(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "first question"),
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "first answer"),
			canonicaltest.Message(t, canonical.MessageRoleUser, "second question"),
		},
	})

	t.Run("foreign Responses continuation uses complete portable input", func(t *testing.T) {
		document, _, err := (codec{}).Encode(provider.Request{
			Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
			PreviousHistory: &provider.PreviousHistory{
				Response: canonical.ResponseRef{SwobuID: "resp_previous", Responses: &canonical.ResponsesContinuation{
					ProviderResponseID: canonical.NewResponsesResponseID("provider_previous"), TargetID: "responses-target", TargetVersion: 1,
				}},
				OmitStart: 0, OmitEnd: 2,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		var payload interactionRequest
		if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.PreviousInteractionID != "" || len(payload.Input) != 3 {
			t.Fatalf("foreign continuation changed Gemini request: %#v", payload)
		}
	})

	for name, previous := range map[string]provider.PreviousHistory{
		"blank provider interaction ID": {
			Response: canonical.ResponseRef{Interactions: &canonical.InteractionsContinuation{TargetID: "gemini-target", TargetVersion: 1}},
		},
		"inverted range": {
			Response:  canonical.ResponseRef{Interactions: &canonical.InteractionsContinuation{ProviderInteractionID: "interaction_previous", TargetID: "gemini-target", TargetVersion: 1}},
			OmitStart: 2, OmitEnd: 1,
		},
		"range beyond request": {
			Response:  canonical.ResponseRef{Interactions: &canonical.InteractionsContinuation{ProviderInteractionID: "interaction_previous", TargetID: "gemini-target", TargetVersion: 1}},
			OmitStart: 0, OmitEnd: 4,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := (codec{}).Encode(provider.Request{
				Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE), PreviousHistory: &previous,
			})
			var internal canonical.Error
			if !errors.As(err, &internal) || internal.Code != canonical.ErrorCodeInternal {
				t.Fatalf("Encode error = %T %v, want INTERNAL_ERROR", err, err)
			}
		})
	}

	t.Run("range starts before request prelude ends", func(t *testing.T) {
		withPrelude := request.WithItems(append([]canonical.CanonicalItem{
			canonicaltest.MustInstruction(canonical.MessageRoleSystem, "current directive"),
		}, request.Items()...))
		previous := provider.PreviousHistory{
			Response:  canonical.ResponseRef{Interactions: &canonical.InteractionsContinuation{ProviderInteractionID: "interaction_previous", TargetID: "gemini-target", TargetVersion: 1}},
			OmitStart: 0, OmitEnd: 2,
		}
		_, _, err := (codec{}).Encode(provider.Request{
			Canonical: withPrelude, Delivery: delivery.StreamingDelivery(delivery.FramingSSE), PreviousHistory: &previous,
		})
		var internal canonical.Error
		if !errors.As(err, &internal) || internal.Code != canonical.ErrorCodeInternal {
			t.Fatalf("Encode error = %T %v, want INTERNAL_ERROR", err, err)
		}
	})
}

func TestCodecLowersNativeControlsOutputImagesReasoningAndFunctions(t *testing.T) {
	maxOutput := 12
	temperature, topP := 0.4, 0.8
	effort := canonical.InferenceEffortXHigh
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: &maxOutput, StopSequences: []string{"END"}, Temperature: &temperature, TopP: &topP, Effort: &effort,
	})
	if err != nil {
		t.Fatal(err)
	}
	budget, err := canonical.NewBudgetReasoningCompute(10_000)
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: canonical.Specify(budget), Disclosure: canonical.Specify(canonical.ReasoningDisclosureSummary),
		ResponsesContext: canonical.Specify(canonical.ResponsesReasoningContextAllTurns),
	})
	if err != nil {
		t.Fatal(err)
	}
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind: canonical.OutputFormatJSONSchema, Name: "reply", Schema: canonical.NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	urlImage, err := canonical.NewURLImage("https://example.test/image.png", canonical.Specify(canonical.ImageDetailHigh))
	if err != nil {
		t.Fatal(err)
	}
	inlineImage, err := canonical.NewInlineImage(canonical.ImageMediaPNG, []byte("PNG"), canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	functionKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	function := canonicaltest.MustFunctionTool(functionKey, "Look up one record", canonicaltest.Schema(t, `{"type":"object","properties":{"q":{"type":"string"}}}`), canonical.Unspecified[bool]())
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"), Controls: controls, Reasoning: reasoning, OutputFormat: canonical.Specify(format),
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &functionKey)),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, function),
			mustGeminiImageMessage(t, "inspect", urlImage, inlineImage),
		},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, changes, err := (codec{}).Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	var payload interactionRequest
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.GenerationConfig == nil || payload.GenerationConfig.MaxOutputTokens == nil || *payload.GenerationConfig.MaxOutputTokens != maxOutput || payload.GenerationConfig.Temperature == nil || *payload.GenerationConfig.Temperature != temperature || payload.GenerationConfig.TopP == nil || *payload.GenerationConfig.TopP != topP || strings.Join(payload.GenerationConfig.StopSequences, ",") != "END" {
		t.Fatalf("generation_config = %#v", payload.GenerationConfig)
	}
	if payload.GenerationConfig.ThinkingLevel != "high" || payload.GenerationConfig.ThinkingSummaries != "auto" || payload.GenerationConfig.ToolChoice.AllowedTools == nil || len(payload.GenerationConfig.ToolChoice.AllowedTools.Tools) != 1 {
		t.Fatalf("generation_config = %#v", payload.GenerationConfig)
	}
	if payload.ResponseFormat == nil || payload.ResponseFormat.Type != "text" || payload.ResponseFormat.MIMEType != "application/json" || string(payload.ResponseFormat.Schema) != `{"type":"object","properties":{"answer":{"type":"string"}}}` {
		t.Fatalf("response_format = %#v", payload.ResponseFormat)
	}
	if len(payload.Tools) != 1 || payload.Tools[0].Type != "function" || payload.Tools[0].Name != "lookup" || string(payload.Tools[0].Parameters) != `{"properties":{"q":{"type":"string"}},"type":"object"}` {
		t.Fatalf("tools = %#v", payload.Tools)
	}
	if len(payload.Input) != 1 || len(payload.Input[0].Content) != 3 || payload.Input[0].Content[1].URI != "https://example.test/image.png" || payload.Input[0].Content[2].Data != "UE5H" || payload.Input[0].Content[2].MIMEType != "image/png" {
		t.Fatalf("input = %#v", payload.Input)
	}
	assertGeminiChanges(t, changes,
		canonical.RequestControlsEffort,
		canonical.RequestReasoningContextResponses,
		canonical.RequestOutputFormatSchema,
		canonical.RequestItemsMessageImageDetail,
	)
}

func TestCodecLowersJSONObjectsAndReasoningTable(t *testing.T) {
	for _, tc := range []struct {
		name        string
		reasoning   canonical.ReasoningControls
		effort      *canonical.InferenceEffort
		wantLevel   string
		wantChanges []compat.Change
	}{
		{name: "omitted"},
		{name: "automatic", reasoning: mustGeminiReasoning(t, canonical.NewAutomaticReasoningCompute())},
		{name: "budget low", reasoning: mustGeminiBudgetReasoning(t, 1_024), wantLevel: "low", wantChanges: []compat.Change{geminiReasoningApproximation()}},
		{name: "budget medium", reasoning: mustGeminiBudgetReasoning(t, 8_192), wantLevel: "medium", wantChanges: []compat.Change{geminiReasoningApproximation()}},
		{name: "budget high", reasoning: mustGeminiBudgetReasoning(t, 24_576), wantLevel: "high", wantChanges: []compat.Change{geminiReasoningApproximation()}},
		{name: "minimal effort", effort: geminiEffort(canonical.InferenceEffortMinimal), wantLevel: "low", wantChanges: []compat.Change{geminiEffortApproximation()}},
		{name: "low effort", effort: geminiEffort(canonical.InferenceEffortLow), wantLevel: "low"},
		{name: "medium effort", effort: geminiEffort(canonical.InferenceEffortMedium), wantLevel: "medium"},
		{name: "high effort", effort: geminiEffort(canonical.InferenceEffortHigh), wantLevel: "high"},
		{name: "xhigh effort", effort: geminiEffort(canonical.InferenceEffortXHigh), wantLevel: "high", wantChanges: []compat.Change{geminiEffortApproximation()}},
		{name: "max effort", effort: geminiEffort(canonical.InferenceEffortMax), wantLevel: "high", wantChanges: []compat.Change{geminiEffortApproximation()}},
		{name: "automatic preserves explicit low", reasoning: mustGeminiReasoning(t, canonical.NewAutomaticReasoningCompute()), effort: geminiEffort(canonical.InferenceEffortLow), wantLevel: "low"},
		{name: "explicit minimal dominates budget", reasoning: mustGeminiBudgetReasoning(t, 24_576), effort: geminiEffort(canonical.InferenceEffortMinimal), wantLevel: "low", wantChanges: []compat.Change{geminiReasoningApproximation(), geminiEffortApproximation()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: tc.effort})
			if err != nil {
				t.Fatal(err)
			}
			format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONObject})
			if err != nil {
				t.Fatal(err)
			}
			document, changes, err := (codec{}).Encode(provider.Request{Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("gemini-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}, Controls: controls, Reasoning: tc.reasoning, OutputFormat: canonical.Specify(format),
			}), Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			var payload interactionRequest
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.ResponseFormat == nil || payload.ResponseFormat.Schema != nil {
				t.Fatalf("response_format = %#v", payload.ResponseFormat)
			}
			level := ""
			if payload.GenerationConfig != nil {
				level = payload.GenerationConfig.ThinkingLevel
			}
			if level != tc.wantLevel {
				t.Fatalf("thinking_level = %q, want %q", level, tc.wantLevel)
			}
			if !reflect.DeepEqual(changes, tc.wantChanges) {
				t.Fatalf("changes = %#v, want %#v", changes, tc.wantChanges)
			}
		})
	}
}

func TestCodecOmitsDisabledReasoningWithoutGeminiOffRepresentation(t *testing.T) {
	for _, effort := range []*canonical.InferenceEffort{nil, geminiEffort(canonical.InferenceEffortMax)} {
		controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: effort})
		if err != nil {
			t.Fatal(err)
		}
		document, changes, err := (codec{}).Encode(provider.Request{Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:     canonical.Specify("gemini-model"),
			Items:     []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
			Controls:  controls,
			Reasoning: mustGeminiReasoning(t, canonical.NewDisabledReasoningCompute()),
		}), Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
		if err != nil || len(document.RawBytes()) == 0 || !containsGeminiChange(changes, canonical.RequestReasoning) {
			t.Fatalf("document=%s changes=%#v err=%v", document.RawBytes(), changes, err)
		}
	}
}

func TestCodecLowersEveryNativeFunctionPolicy(t *testing.T) {
	functionKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	function := canonicaltest.MustFunctionTool(functionKey, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	for _, tc := range []struct {
		name      string
		policy    canonical.Specified[canonical.ToolPolicy]
		wantMode  string
		wantNamed bool
	}{
		{name: "effective auto", wantMode: "auto"},
		{name: "none", policy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyNone, nil)), wantMode: "none"},
		{name: "auto", policy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil)), wantMode: "auto"},
		{name: "required", policy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)), wantMode: "any"},
		{name: "specific", policy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &functionKey)), wantNamed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("gemini-model"), ToolPolicy: tc.policy,
				Items: []canonical.CanonicalItem{
					canonicaltest.ToolDeclarations(t, function), canonicaltest.Message(t, canonical.MessageRoleUser, "hello"),
				},
			})
			names, _, err := provider.BuildAttemptToolNames(request)
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := (codec{}).Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			var payload interactionRequest
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.GenerationConfig == nil || payload.GenerationConfig.ToolChoice == nil {
				t.Fatalf("generation_config = %#v", payload.GenerationConfig)
			}
			choice := payload.GenerationConfig.ToolChoice
			if tc.wantNamed {
				if choice.AllowedTools == nil || choice.AllowedTools.Mode != "any" || len(choice.AllowedTools.Tools) != 1 || choice.AllowedTools.Tools[0] != "lookup" {
					t.Fatalf("specific tool choice = %#v", choice)
				}
				return
			}
			if choice.Mode != tc.wantMode || choice.AllowedTools != nil {
				t.Fatalf("tool choice = %#v, want %q", choice, tc.wantMode)
			}
		})
	}
}

func TestGeminiSpecificPolicyConsumesEmittedProjectionIdentity(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "canonical-name")
	policy := canonical.NewToolPolicy(canonical.ToolPolicySpecific, &key)
	lowered := wire.LoweredToolSet{Records: []wire.LoweredToolRecord{{
		Key: key, Kind: key.Kind(), FragmentCount: 1, TargetType: "function", TargetName: "emitted-name",
	}}}

	choice, represented, err := geminiToolChoice(policy, lowered)
	if err != nil {
		t.Fatal(err)
	}
	if !represented || choice.AllowedTools == nil || len(choice.AllowedTools.Tools) != 1 || choice.AllowedTools.Tools[0] != "emitted-name" {
		t.Fatalf("specific choice = %#v represented=%t, want emitted projection identity", choice, represented)
	}
}

func TestCodecLowersNativeGoogleSearch(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, canonical.NewWebSearchDeclaration()),
			canonicaltest.Message(t, canonical.MessageRoleUser, "find the docs"),
		},
	})
	document, changes, err := (codec{}).Encode(provider.Request{
		Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v, want exact Google Search lowering", changes)
	}
	var payload struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Tools) != 1 {
		t.Fatalf("tools = %s, want Google Search", payload.Tools)
	}
	var search map[string]any
	if err := json.Unmarshal(payload.Tools[0], &search); err != nil {
		t.Fatal(err)
	}
	if search["type"] != "google_search" || len(search) != 2 {
		t.Fatalf("search = %#v, want explicit web-only Google Search", search)
	}
	if searchTypes, ok := search["search_types"].([]any); !ok || len(searchTypes) != 1 || searchTypes[0] != "web_search" {
		t.Fatalf("search_types = %#v, want only web_search", search["search_types"])
	}
}

func TestCodecOmitsSettledPortableGoogleSearchHistory(t *testing.T) {
	callID, err := canonical.NewToolCallID("search_portable")
	if err != nil {
		t.Fatal(err)
	}
	input, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{
		Action:  canonical.WebSearchActionSearch,
		Queries: []string{"swobu"},
	})
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	searchResult, err := canonical.NewWebSearchResult(nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewWebSearchResultItem(callID, searchResult)
	if err != nil {
		t.Fatal(err)
	}
	current := canonicaltest.Message(t, canonical.MessageRoleUser, "continue")
	providerDelivery := delivery.StreamingDelivery(delivery.FramingSSE)

	document, changes, err := (codec{}).Encode(provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gemini-model"),
			Items: []canonical.CanonicalItem{call, result, current},
		}),
		Delivery: providerDelivery,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(document.RawBytes()), "google_search_call") || strings.Contains(string(document.RawBytes()), "google_search_result") {
		t.Fatalf("settled portable Search history leaked into Gemini request: %s", document.RawBytes())
	}
	if !strings.Contains(string(document.RawBytes()), `"text":"continue"`) {
		t.Fatalf("current turn was lost: %s", document.RawBytes())
	}
	wantChange := compat.NewOmission(canonical.RequestItemsKind, canonical.RequestItemOccurrence(0))
	if len(changes) != 1 || changes[0] != wantChange {
		t.Fatalf("changes = %#v, want %#v", changes, wantChange)
	}

	document, changes, err = (codec{}).Encode(provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gemini-model"),
			Items: []canonical.CanonicalItem{call, current},
		}),
		Delivery: providerDelivery,
	})
	if err != nil {
		t.Fatalf("unsettled Search Encode error = %v", err)
	}
	if strings.Contains(string(document.RawBytes()), "google_search_call") {
		t.Fatalf("unsettled portable Search leaked into Gemini request: %s", document.RawBytes())
	}
	wantChange = compat.NewOmission(canonical.RequestItemsKind, canonical.RequestItemOccurrence(0))
	if len(changes) != 1 || changes[0] != wantChange {
		t.Fatalf("unsettled changes = %#v, want %#v", changes, wantChange)
	}

	exactCall, err := canonical.NewInteractionsWebSearchCall(canonical.WebSearchCall{
		Action:  canonical.WebSearchActionSearch,
		Queries: []string{"swobu"},
	}, []byte(`{"type":"google_search_call","id":"search_portable","arguments":{"queries":["swobu"]},"search_type":"web_search","signature":"sig"}`))
	if err != nil {
		t.Fatal(err)
	}
	exactInput, err := canonical.NewWebSearchToolInput(exactCall)
	if err != nil {
		t.Fatal(err)
	}
	exactCallItem, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), exactInput)
	if err != nil {
		t.Fatal(err)
	}
	document, changes, err = (codec{}).Encode(provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gemini-model"),
			Items: []canonical.CanonicalItem{exactCallItem, result, current},
		}),
		Delivery: providerDelivery,
	})
	if err != nil {
		t.Fatalf("partially exact Search Encode error = %v", err)
	}
	if strings.Contains(string(document.RawBytes()), "google_search_call") || strings.Contains(string(document.RawBytes()), "google_search_result") {
		t.Fatalf("partially exact Search leaked into Gemini request: %s", document.RawBytes())
	}
	if len(changes) != 1 || changes[0] != wantChange {
		t.Fatalf("partially exact changes = %#v, want %#v", changes, wantChange)
	}
}

func TestSettledPortableSearchProjectionFailsClosedAndPreservesOccurrences(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search_reused")
	portableCall := mustPortableGeminiSearchCall(t, callID)
	portableResult := mustPortableGeminiSearchResult(t, callID)
	exactCall := mustExactGeminiSearchCall(t, callID)
	exactResult := mustExactGeminiSearchResult(t, callID)
	current := canonicaltest.Message(t, canonical.MessageRoleUser, "continue")

	for _, tc := range []struct {
		name     string
		items    []canonical.CanonicalItem
		wantCode canonical.ErrorCode
	}{
		{name: "orphan result", items: []canonical.CanonicalItem{portableResult, current}, wantCode: canonical.ErrorCodeInternal},
		{name: "duplicate result", items: []canonical.CanonicalItem{portableCall, portableResult, portableResult, current}, wantCode: canonical.ErrorCodeInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := projectSettledPortableSearchHistory(canonical.NewCanonicalRequest(canonical.RequestParams{Items: tc.items}))
			var swobuErr canonical.Error
			if !errors.As(err, &swobuErr) || swobuErr.Code != tc.wantCode {
				t.Fatalf("projection error = %T %v, want %s", err, err, tc.wantCode)
			}
		})
	}

	for _, tc := range []struct {
		name  string
		items []canonical.CanonicalItem
	}{
		{name: "unresolved call", items: []canonical.CanonicalItem{portableCall, current}},
		{name: "exact call portable result", items: []canonical.CanonicalItem{exactCall, portableResult, current}},
		{name: "portable call exact result", items: []canonical.CanonicalItem{portableCall, exactResult, current}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projected, changes, err := projectSettledPortableSearchHistory(canonical.NewCanonicalRequest(canonical.RequestParams{Items: tc.items}))
			if err != nil {
				t.Fatal(err)
			}
			if len(projected.Items()) != 1 || len(changes) != 1 || changes[0] != compat.NewOmission(canonical.RequestItemsKind, canonical.RequestItemOccurrence(0)) {
				t.Fatalf("projection = (%#v, %#v), want current item plus call omission", projected.Items(), changes)
			}
		})
	}

	projected, changes, err := projectSettledPortableSearchHistory(canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: []canonical.CanonicalItem{portableCall, portableResult, portableCall, portableResult, current},
	}))
	if err != nil {
		t.Fatal(err)
	}
	wantChanges := []compat.Change{
		compat.NewOmission(canonical.RequestItemsKind, canonical.RequestItemOccurrence(0)),
		compat.NewOmission(canonical.RequestItemsKind, canonical.RequestItemOccurrence(2)),
	}
	if !reflect.DeepEqual(changes, wantChanges) {
		t.Fatalf("changes = %#v, want %#v", changes, wantChanges)
	}
	if len(projected.Items()) != 1 {
		t.Fatalf("projected items = %#v, want current message only", projected.Items())
	}
}

func TestGeminiFunctionResultProjectsImageContentWithLossEvidence(t *testing.T) {
	image, err := canonical.NewURLImage("https://example.test/result.png", canonical.Specify(canonical.ImageDetailHigh))
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := canonical.NewToolCallID("call_image")
	resultItem, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{
		canonical.NewTextToolResultPart("before"),
		canonical.NewImageToolResultPart(image),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := resultItem.ToolResult()
	if !ok {
		t.Fatal("tool result constructor returned another item kind")
	}

	step, changes, err := geminiFunctionResult(result, "inspect", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(step.Result) != 2 || step.Result[0].Text != "before" || step.Result[1].URI != "https://example.test/result.png" {
		t.Fatalf("function result = %#v", step.Result)
	}
	want := []compat.Change{
		compat.NewApproximation(canonical.RequestItemsToolResultImage, canonical.RequestItemOccurrence(3)),
		compat.NewApproximation(canonical.RequestItemsToolResultImageDetail, canonical.RequestItemOccurrence(3)),
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes = %#v, want %#v", changes, want)
	}
}

func mustPortableGeminiSearchCall(t *testing.T, callID canonical.ToolCallID) canonical.CanonicalItem {
	t.Helper()
	input, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"swobu"}})
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func mustPortableGeminiSearchResult(t *testing.T, callID canonical.ToolCallID) canonical.CanonicalItem {
	t.Helper()
	result, err := canonical.NewWebSearchResult(nil)
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewWebSearchResultItem(callID, result)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func mustExactGeminiSearchCall(t *testing.T, callID canonical.ToolCallID) canonical.CanonicalItem {
	t.Helper()
	call, err := canonical.NewInteractionsWebSearchCall(
		canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"swobu"}},
		[]byte(`{"type":"google_search_call","id":"search_reused","arguments":{"queries":["swobu"]},"search_type":"web_search","signature":"sig-call"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	input, err := canonical.NewWebSearchToolInput(call)
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func mustExactGeminiSearchResult(t *testing.T, callID canonical.ToolCallID) canonical.CanonicalItem {
	t.Helper()
	result, err := canonical.NewWebSearchResult(nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err = result.WithInteractionsReplay([]byte(`{"type":"google_search_result","call_id":"search_reused","result":[],"signature":"sig-result"}`))
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewWebSearchResultItem(callID, result)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestCodecRejectsResidualMCPBeforeProviderLowering(t *testing.T) {
	mcpKey, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "connector")
	if err != nil {
		t.Fatal(err)
	}
	source, err := canonical.NewMCPConnectorSource(
		"connector-1", canonical.Unspecified[[]string](), canonical.NewMCPApprovalNever(), canonical.MCPLoadingEager, canonical.Unspecified[[]string](),
	)
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := canonical.NewMCPToolSource(mcpKey, "", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, declaration), canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
	})
	_, _, err = (codec{}).Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	var swobuErr canonical.Error
	if !errors.As(err, &swobuErr) || swobuErr.Code != canonical.ErrorCodeInternal {
		t.Fatalf("Encode error = %T %v, want INTERNAL_ERROR", err, err)
	}
}

func TestCodecProjectsStrictJSONSchemaWithoutStrengtheningApproximation(t *testing.T) {
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind: canonical.OutputFormatJSONSchema, Name: "reply", Strict: true,
		Schema: canonical.NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	document, changes, err := (codec{}).Encode(provider.Request{Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"), OutputFormat: canonical.Specify(format),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
	}), Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v, want exact strict schema projection", changes)
	}
	var payload interactionRequest
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ResponseFormat == nil || string(payload.ResponseFormat.Schema) != `{"type":"object","properties":{"answer":{"type":"string"}}}` {
		t.Fatalf("response_format = %#v", payload.ResponseFormat)
	}
}

func TestCodecRejectsUnrepresentableToolAndReplaySemanticsBeforeDispatch(t *testing.T) {
	callID, err := canonical.NewToolCallID("call_lookup")
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("result")}, false)
	if err != nil {
		t.Fatal(err)
	}
	customKey := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "shell")
	base := func() canonical.RequestParams {
		return canonical.RequestParams{Model: canonical.Specify("gemini-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*canonical.RequestParams)
	}{
		{name: "custom", mutate: func(params *canonical.RequestParams) {
			params.Items = []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, canonicaltest.MustCustomTool(customKey, "", canonicaltest.MustToolFormat(`{"type":"text"}`))), canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := base()
			tc.mutate(&params)
			_, changes, err := (codec{}).Encode(provider.Request{Canonical: canonical.NewCanonicalRequest(params), Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			want := compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(customKey))
			if len(changes) != 1 || changes[0] != want {
				t.Fatalf("changes = %#v, want %#v", changes, want)
			}
		})
	}
	_, _, err = (codec{}).Encode(provider.Request{Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gemini-model"), Items: []canonical.CanonicalItem{result}}), Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err == nil || !strings.Contains(err.Error(), "orphan result") {
		t.Fatalf("orphan function result error = %v", err)
	}
}

func TestCodecEvaluatesToolPolicyAfterNativeToolProjection(t *testing.T) {
	customKey := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "shell")
	functionKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	custom := canonicaltest.MustCustomTool(customKey, "", canonicaltest.MustToolFormat(`{"type":"text"}`))
	function := canonicaltest.MustFunctionTool(functionKey, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	user := canonicaltest.Message(t, canonical.MessageRoleUser, "hello")

	for _, tc := range []struct {
		name               string
		declarations       []canonical.ToolDeclaration
		policy             canonical.ToolPolicy
		batch              canonical.Specified[canonical.ToolCallBatchPolicy]
		wantPolicyOmission bool
		wantChoiceMode     string
		wantTools          int
		wantBatchOmission  bool
	}{
		{
			name:               "required with only omitted custom tool",
			declarations:       []canonical.ToolDeclaration{custom},
			policy:             canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
			wantPolicyOmission: true,
		},
		{
			name:         "auto with only omitted custom tool",
			declarations: []canonical.ToolDeclaration{custom},
			policy:       canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil),
		},
		{
			name:           "required with supported function and omitted custom tool",
			declarations:   []canonical.ToolDeclaration{function, custom},
			policy:         canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
			wantChoiceMode: "any",
			wantTools:      1,
		},
		{
			name:         "at most one with only omitted custom tool",
			declarations: []canonical.ToolDeclaration{custom},
			policy:       canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil),
			batch:        canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model:         canonical.Specify("gemini-model"),
				Items:         []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tc.declarations...), user},
				ToolPolicy:    canonical.Specify(tc.policy),
				ToolCallBatch: tc.batch,
			})
			names, _, err := provider.BuildAttemptToolNames(request)
			if err != nil {
				t.Fatal(err)
			}
			document, changes, err := (codec{}).Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			var payload interactionRequest
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Tools) != tc.wantTools {
				t.Fatalf("tools = %#v, want %d", payload.Tools, tc.wantTools)
			}
			if tc.wantPolicyOmission != containsGeminiChange(changes, canonical.RequestToolPolicy) {
				t.Fatalf("policy omission = %t, changes=%#v", tc.wantPolicyOmission, changes)
			}
			choiceMode := ""
			if payload.GenerationConfig != nil && payload.GenerationConfig.ToolChoice != nil {
				choiceMode = payload.GenerationConfig.ToolChoice.Mode
			}
			if choiceMode != tc.wantChoiceMode {
				t.Fatalf("tool choice mode = %q, want %q", choiceMode, tc.wantChoiceMode)
			}
			batchOmitted := false
			for _, change := range changes {
				if change == compat.NewOmission(canonical.RequestToolCallBatch, canonical.Occurrence{}) {
					batchOmitted = true
				}
			}
			if batchOmitted != tc.wantBatchOmission {
				t.Fatalf("batch omission = %t, want %t; changes = %#v", batchOmitted, tc.wantBatchOmission, changes)
			}
		})
	}
}

func TestGeminiCodecToolCallBatchAtMostOneSemantics(t *testing.T) {
	functionKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	toolDecl := canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(functionKey, "lookup function", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]()))
	userMsg := canonicaltest.Message(t, canonical.MessageRoleUser, "hello")

	encodeReq := func(t *testing.T, req canonical.CanonicalRequest) []compat.Change {
		t.Helper()
		names, _, err := provider.BuildAttemptToolNames(req)
		if err != nil {
			t.Fatal(err)
		}
		_, changes, err := (codec{}).Encode(provider.Request{Canonical: req, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		return changes
	}

	t.Run("without tools remains exact", func(t *testing.T) {
		req := canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:         canonical.Specify("gemini-model"),
			Items:         []canonical.CanonicalItem{userMsg},
			ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
		})
		changes := encodeReq(t, req)
		if len(changes) != 0 {
			t.Fatalf("changes = %#v, want 0 changes (exact)", changes)
		}
	})

	t.Run("with tool_choice none remains exact", func(t *testing.T) {
		req := canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:         canonical.Specify("gemini-model"),
			Items:         []canonical.CanonicalItem{toolDecl, userMsg},
			ToolPolicy:    canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyNone, nil)),
			ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
		})
		changes := encodeReq(t, req)
		if len(changes) != 0 {
			t.Fatalf("changes = %#v, want 0 changes (exact)", changes)
		}
	})

	t.Run("with active tools and auto tool_choice succeeds with omission", func(t *testing.T) {
		req := canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:         canonical.Specify("gemini-model"),
			Items:         []canonical.CanonicalItem{toolDecl, userMsg},
			ToolPolicy:    canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil)),
			ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
		})
		changes := encodeReq(t, req)
		wantChange := compat.NewOmission(canonical.RequestToolCallBatch, canonical.Occurrence{})
		if len(changes) != 1 || changes[0] != wantChange {
			t.Fatalf("changes = %#v, want [%#v]", changes, wantChange)
		}
	})

	t.Run("with active tools and required tool_choice succeeds with omission", func(t *testing.T) {
		req := canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:         canonical.Specify("gemini-model"),
			Items:         []canonical.CanonicalItem{toolDecl, userMsg},
			ToolPolicy:    canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
			ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
		})
		changes := encodeReq(t, req)
		wantChange := compat.NewOmission(canonical.RequestToolCallBatch, canonical.Occurrence{})
		if len(changes) != 1 || changes[0] != wantChange {
			t.Fatalf("changes = %#v, want [%#v]", changes, wantChange)
		}
	})

	t.Run("with active tools and specific tool_choice succeeds with omission", func(t *testing.T) {
		req := canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:         canonical.Specify("gemini-model"),
			Items:         []canonical.CanonicalItem{toolDecl, userMsg},
			ToolPolicy:    canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &functionKey)),
			ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
		})
		changes := encodeReq(t, req)
		wantChange := compat.NewOmission(canonical.RequestToolCallBatch, canonical.Occurrence{})
		if len(changes) != 1 || changes[0] != wantChange {
			t.Fatalf("changes = %#v, want [%#v]", changes, wantChange)
		}
	})
}

func TestGeminiFunctionStrictnessDegradesOnlyWhenTrue(t *testing.T) {
	functionKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	user := canonicaltest.Message(t, canonical.MessageRoleUser, "hello")
	for _, tc := range []struct {
		name         string
		strict       canonical.Specified[bool]
		wantOmission bool
	}{
		{name: "unspecified", strict: canonical.Unspecified[bool]()},
		{name: "explicit false", strict: canonical.Specify(false)},
		{name: "explicit true", strict: canonical.Specify(true), wantOmission: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			declaration := canonicaltest.MustFunctionTool(functionKey, "lookup", canonicaltest.Schema(t, `{"type":"object"}`), tc.strict)
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("gemini-model"), Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, declaration), user},
			})
			names, _, err := provider.BuildAttemptToolNames(request)
			if err != nil {
				t.Fatal(err)
			}
			_, changes, err := (codec{}).Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantOmission != containsGeminiChange(changes, canonical.RequestToolsSchemaStrict) {
				t.Fatalf("strict omission = %t, changes=%#v", tc.wantOmission, changes)
			}
		})
	}
}

func mustGeminiImageMessage(t *testing.T, text string, images ...canonical.ImagePart) canonical.CanonicalItem {
	t.Helper()
	parts := []canonical.MessagePart{canonical.NewTextMessagePart(text)}
	for _, image := range images {
		parts = append(parts, canonical.NewImageMessagePart(image))
	}
	item, err := canonical.NewMessageItem(canonical.MessageRoleUser, parts)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func assertGeminiChanges(t *testing.T, changes []compat.Change, capabilities ...canonical.CapabilityPath) {
	t.Helper()
	for _, capability := range capabilities {
		found := false
		for _, change := range changes {
			if change.Capability == capability {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("changes = %#v, missing %s", changes, capability)
		}
	}
}

func containsGeminiChange(changes []compat.Change, capability canonical.CapabilityPath) bool {
	for _, change := range changes {
		if change.Capability == capability {
			return true
		}
	}
	return false
}

func mustGeminiReasoning(t *testing.T, compute canonical.ReasoningCompute) canonical.ReasoningControls {
	t.Helper()
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(compute)})
	if err != nil {
		t.Fatal(err)
	}
	return reasoning
}

func mustGeminiBudgetReasoning(t *testing.T, tokens int) canonical.ReasoningControls {
	t.Helper()
	compute, err := canonical.NewBudgetReasoningCompute(tokens)
	if err != nil {
		t.Fatal(err)
	}
	return mustGeminiReasoning(t, compute)
}

func geminiEffort(value canonical.InferenceEffort) *canonical.InferenceEffort { return &value }

func geminiReasoningApproximation() compat.Change {
	return compat.NewApproximation(canonical.RequestReasoning, canonical.Occurrence{})
}

func TestCodecRejectsNonSSEProviderDelivery(t *testing.T) {
	_, _, err := (codec{}).Encode(provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gemini-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		Delivery: delivery.BufferedDelivery(),
	})
	var swobuErr canonical.Error
	if !errors.As(err, &swobuErr) || swobuErr.Code != canonical.ErrorCodeBadEndpoint {
		t.Fatalf("Encode error = %T %v, want BAD_ENDPOINT", err, err)
	}
}
