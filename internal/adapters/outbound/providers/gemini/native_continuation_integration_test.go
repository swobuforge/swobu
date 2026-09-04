package gemini

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

// TestStoredGeminiContinuationComposesSessionAuthorityWithPrivateLowering
// proves the only native Gemini continuation path: session grants an exact
// typed relation and the private codec turns it into Google wire state.
func TestStoredGeminiContinuationComposesSessionAuthorityWithPrivateLowering(t *testing.T) {
	target := geminiTarget()
	turnOne := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(target.Model),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "turn one"),
		},
	})
	turnOneResponse := canonicaltest.ResponseWithRef(t, canonical.ResponseRef{
		SwobuID: "resp_previous",
		Interactions: &canonical.InteractionsContinuation{
			ProviderInteractionID: canonical.NewInteractionID("interaction_previous"),
			TargetID:              target.TargetID,
			TargetVersion:         target.TargetVersion,
		},
	}, target.Model, []canonical.CanonicalItem{
		canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer one"),
	}, canonical.Completed("completed"), canonical.NewUnknownTokenUsage())
	checkpoint := continuity.Checkpoint{Request: turnOne, Response: turnOneResponse}

	newTurn := func(store canonical.Specified[bool]) canonical.CanonicalRequest {
		return canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify(target.Model), Store: store,
			Items: []canonical.CanonicalItem{
				canonicaltest.MustInstruction(canonical.MessageRoleDeveloper, "current directive"),
				canonicaltest.Message(t, canonical.MessageRoleUser, "turn two"),
			},
			PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
		})
	}

	t.Run("same target lowers native interaction and keeps current material", func(t *testing.T) {
		resolved, err := continuity.Resume(newTurn(canonical.Unspecified[bool]()), checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		previous, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion)
		if !ok || previous.Response.Interactions == nil {
			t.Fatalf("PreviousHistory = (%#v, %t), want typed Gemini authority", previous, ok)
		}
		payload := encodeGeminiContinuationRequest(t, resolved.Request(), &previous)
		if payload.PreviousInteractionID != "interaction_previous" || payload.SystemInstruction != "current directive" {
			t.Fatalf("same-target payload = %#v", payload)
		}
		if len(payload.Input) != 1 || payload.Input[0].Type != "user_input" || payload.Input[0].Content[0].Text != "turn two" {
			t.Fatalf("same-target input = %#v, want current turn only", payload.Input)
		}
	})

	for _, tc := range []struct {
		name    string
		target  provider.TargetSnapshot
		store   canonical.Specified[bool]
		wantSet bool
	}{
		{name: "target ID change", target: func() provider.TargetSnapshot { changed := target; changed.TargetID = "gemini-other"; return changed }()},
		{name: "target version change", target: func() provider.TargetSnapshot { changed := target; changed.TargetVersion++; return changed }()},
		{name: "store false", target: target, store: canonical.Specify(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := continuity.Resume(newTurn(tc.store), checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			previous, ok := resolved.PreviousHistory(tc.target.TargetID, tc.target.TargetVersion)
			if ok != tc.wantSet {
				t.Fatalf("PreviousHistory = (%#v, %t), want present=%t", previous, ok, tc.wantSet)
			}
			var native *provider.PreviousHistory
			if ok {
				native = &previous
			}
			payload := encodeGeminiContinuationRequest(t, resolved.Request(), native)
			if payload.PreviousInteractionID != "" {
				t.Fatalf("fallback payload carried native interaction: %#v", payload)
			}
			if tc.name == "store false" && (payload.Store == nil || *payload.Store) {
				t.Fatalf("store:false fallback did not forward persistence intent: %#v", payload)
			}
			if len(payload.Input) != 3 || payload.Input[0].Content[0].Text != "turn one" || payload.Input[1].Content[0].Text != "answer one" || payload.Input[2].Content[0].Text != "turn two" {
				t.Fatalf("fallback input = %#v, want full portable history", payload.Input)
			}
		})
	}
}

// TestStoredGeminiFunctionContinuationPreservesCurrentScopeAndCorrelation
// proves that native continuation compresses the prefix while retaining the
// same explicit caller result that exact stateless replay can carry.
func TestStoredGeminiFunctionContinuationPreservesCurrentScopeAndCorrelation(t *testing.T) {
	target := geminiTarget()
	functionKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	function := canonicaltest.MustFunctionTool(functionKey, "", canonicaltest.Schema(t, `{"type":"object","properties":{"q":{"type":"string"}}}`), canonical.Unspecified[bool]())
	callID, err := canonical.NewToolCallID("call_lookup")
	if err != nil {
		t.Fatal(err)
	}
	call := canonicaltest.ToolCall(t, callID.String(), functionKey, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"swobu"}`)))
	firstRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(target.Model),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, function),
			canonicaltest.Message(t, canonical.MessageRoleUser, "look it up"),
		},
	})
	firstResponse := canonicaltest.ResponseWithRef(t, canonical.ResponseRef{
		SwobuID: "resp_function_previous",
		Interactions: &canonical.InteractionsContinuation{
			ProviderInteractionID: canonical.NewInteractionID("interaction_function_previous"), TargetID: target.TargetID, TargetVersion: target.TargetVersion,
		},
	}, target.Model, []canonical.CanonicalItem{call}, canonical.Completed("requires_action"), canonical.NewUnknownTokenUsage())
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart(`{"result":"found"}`)}, true)
	if err != nil {
		t.Fatal(err)
	}
	maxOutput, temperature := 321, 0.2
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{MaxOutputTokens: &maxOutput, Temperature: &temperature})
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Disclosure: canonical.Specify(canonical.ReasoningDisclosureNone)})
	if err != nil {
		t.Fatal(err)
	}
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONObject})
	if err != nil {
		t.Fatal(err)
	}
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    canonical.Specify(target.Model),
		Controls: controls, Reasoning: reasoning, OutputFormat: canonical.Specify(format),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, function),
			result,
		},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_function_previous"},
	})
	resolved, err := continuity.Resume(current, continuity.Checkpoint{Request: firstRequest, Response: firstResponse})
	if err != nil {
		t.Fatal(err)
	}
	previous, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion)
	if !ok || previous.Response.Interactions == nil {
		t.Fatalf("PreviousHistory = (%#v, %t), want Gemini native continuation", previous, ok)
	}
	names, _, err := provider.BuildAttemptToolNames(resolved.Request())
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := (codec{}).Encode(provider.Request{Canonical: resolved.Request(), PreviousHistory: &previous, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	var payload interactionRequest
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PreviousInteractionID != "interaction_function_previous" || len(payload.Input) != 1 || payload.Input[0].Type != "function_result" || payload.Input[0].CallID != "call_lookup" || payload.Input[0].IsError == nil || !*payload.Input[0].IsError || len(payload.Input[0].Result) != 1 || payload.Input[0].Result[0].Text != `{"result":"found"}` {
		t.Fatalf("function continuation payload = %#v", payload)
	}
	if len(payload.Tools) != 1 || payload.Tools[0].Name != "lookup" || payload.GenerationConfig == nil || payload.GenerationConfig.ToolChoice == nil || payload.GenerationConfig.ToolChoice.Mode != "auto" || payload.GenerationConfig.MaxOutputTokens == nil || *payload.GenerationConfig.MaxOutputTokens != maxOutput || payload.GenerationConfig.Temperature == nil || *payload.GenerationConfig.Temperature != temperature || payload.GenerationConfig.ThinkingSummaries != "none" || payload.ResponseFormat == nil || payload.ResponseFormat.MIMEType != "application/json" {
		t.Fatalf("current function scope = %#v", payload)
	}
	stateless := encodeGeminiContinuationRequest(t, resolved.Request(), nil)
	if stateless.PreviousInteractionID != "" || len(stateless.Input) != 3 || stateless.Input[1].Type != "function_call" || stateless.Input[2].Type != "function_result" || stateless.Input[2].CallID != "call_lookup" {
		t.Fatalf("stateless function continuation payload = %#v", stateless)
	}
}

func encodeGeminiContinuationRequest(t *testing.T, request canonical.CanonicalRequest, previous *provider.PreviousHistory) interactionRequest {
	t.Helper()
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := (codec{}).Encode(provider.Request{
		Canonical: request, PreviousHistory: previous, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload interactionRequest
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}
