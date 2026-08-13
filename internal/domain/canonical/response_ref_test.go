package canonical

import (
	"context"
	"reflect"
	"testing"
)

func TestResponseIdentityDomainsHaveDistinctNominalTypes(t *testing.T) {
	if reflect.TypeOf(SwobuResponseID("")) == reflect.TypeOf(ResponsesResponseID("")) {
		t.Fatal("Swobu and provider response identities share a nominal type")
	}
	if reflect.TypeOf(InteractionID("")) == reflect.TypeOf(ResponsesResponseID("")) {
		t.Fatal("Interactions and Responses provider identities share a nominal type")
	}
}

func TestResponseIdentityConstructorsPreserveOpaqueBytes(t *testing.T) {
	const raw = "  opaque response id  "
	if got := NewSwobuResponseID(raw); string(got) != raw {
		t.Fatalf("Swobu response ID = %q, want exact %q", got, raw)
	}
	if got := NewResponsesResponseID(raw); string(got) != raw {
		t.Fatalf("provider response ID = %q, want exact %q", got, raw)
	}
}

func TestResponseRefBoundaryValidation(t *testing.T) {
	for name, ref := range map[string]ResponseRef{
		"empty":      {},
		"whitespace": {SwobuID: "   "},
	} {
		t.Run("selector/"+name, func(t *testing.T) {
			if err := ref.ValidatePreviousResponseSelector(); err == nil {
				t.Fatal("empty previous-response selector accepted")
			}
		})
		t.Run("committed/"+name, func(t *testing.T) {
			if err := ref.ValidateCommittedResponse(); err == nil {
				t.Fatal("empty committed response accepted")
			}
		})
	}
}

func TestResponsesContinuationValidateBound(t *testing.T) {
	valid := ResponsesContinuation{ProviderResponseID: "provider_resp_789", TargetID: "target-a", TargetVersion: 7}
	if err := valid.ValidateBound(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ResponsesContinuation){
		"provider ID":    func(ref *ResponsesContinuation) { ref.ProviderResponseID = "" },
		"target ID":      func(ref *ResponsesContinuation) { ref.TargetID = "" },
		"target version": func(ref *ResponsesContinuation) { ref.TargetVersion = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := candidate.ValidateBound(); err == nil {
				t.Fatalf("invalid native ref accepted: %#v", candidate)
			}
		})
	}
}

func TestInteractionsContinuationIsBoundTypedAndCloned(t *testing.T) {
	continuation := InteractionsContinuation{ProviderInteractionID: NewInteractionID("interaction_789"), TargetID: "target-a", TargetVersion: 7}
	if err := continuation.ValidateBound(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*InteractionsContinuation){
		"provider ID":    func(ref *InteractionsContinuation) { ref.ProviderInteractionID = "" },
		"target ID":      func(ref *InteractionsContinuation) { ref.TargetID = "" },
		"target version": func(ref *InteractionsContinuation) { ref.TargetVersion = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := continuation
			mutate(&candidate)
			if err := candidate.ValidateBound(); err == nil {
				t.Fatalf("invalid native ref accepted: %#v", candidate)
			}
		})
	}

	ref := ResponseRef{SwobuID: NewSwobuResponseID("resp_1"), Interactions: &continuation}
	clone := ref.Clone()
	clone.Interactions.TargetID = "target-b"
	if ref.Interactions.TargetID != "target-a" {
		t.Fatal("response reference clone aliases Interactions continuation")
	}
	ref.Responses = &ResponsesContinuation{ProviderResponseID: NewResponsesResponseID("response_1"), TargetID: "target-a", TargetVersion: 7}
	if err := ref.ValidateCommittedResponse(); err == nil {
		t.Fatal("committed response accepted two native continuation families")
	}
}

func TestBoundResponseIdentityStreamBindsInteractionsContinuation(t *testing.T) {
	bound := NewBoundResponseIdentityStream(NewSliceEventReader([]Event{{
		Kind: EventResponseIdentity,
		Payload: ResponseIdentityPayload{Response: ResponseRef{
			Interactions: &InteractionsContinuation{ProviderInteractionID: NewInteractionID("interaction_789")},
		}},
		Meta: EventMetadataFields{NativeID: "interaction_789"},
	}}), ResponseBinding{SwobuID: NewSwobuResponseID("resp_1"), TargetID: "target-a", TargetVersion: 7})
	event, err := bound.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := event.Payload.(ResponseIdentityPayload)
	if !ok || payload.Response.SwobuID != "resp_1" || payload.Response.Interactions == nil {
		t.Fatalf("bound identity = %#v", event)
	}
	if got := payload.Response.Interactions; got.TargetID != "target-a" || got.TargetVersion != 7 || got.ProviderInteractionID != "interaction_789" {
		t.Fatalf("bound Interactions continuation = %#v", got)
	}
	if event.Meta.NativeID != "" {
		t.Fatalf("bound event leaked native metadata: %#v", event.Meta)
	}
}

// TestResponsesItemIDIsDistinctFromCorrelationAndResponseID locks the identity
// separation at the heart of the second release blocker: a Responses item id is
// a provider-owned presentation identity, distinct in nominal type from both the
// canonical call correlation token (ToolCallID) and the provider response id.
func TestResponsesItemIDIsDistinctFromCorrelationAndResponseID(t *testing.T) {
	if reflect.TypeOf(ResponsesItemID("")) == reflect.TypeOf(ResponsesResponseID("")) {
		t.Fatal("Responses item id shares a nominal type with provider response id")
	}
	if reflect.TypeOf(ResponsesItemID("")) == reflect.TypeOf(ToolCallID{}) {
		t.Fatal("Responses item id shares a nominal type with canonical ToolCallID")
	}
}

func TestResponsesItemIDRejectsBlankAndWhitespace(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":      "",
		"whitespace": "   ",
		"padded":     " ws_1 ",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewResponsesItemID(raw); err == nil {
				t.Fatalf("blank/padded item id accepted: %q", raw)
			}
		})
	}
	if id, err := NewResponsesItemID("ws_1"); err != nil || id.IsZero() || id.String() != "ws_1" {
		t.Fatalf("valid item id not preserved exactly: %v %q", err, id)
	}
}

// TestResponsesWebSearchRefinementIsOptInAndIndependentOfCorrelation proves the
// refinement is an opt-in exact-id carrier: a web-search call constructed
// without it has none (the idless replay case), while one constructed with it
// preserves the exact provider id without disturbing the correlation token.
func TestResponsesWebSearchRefinementIsOptInAndIndependentOfCorrelation(t *testing.T) {
	input, err := NewWebSearchToolInput(WebSearchCall{Action: WebSearchActionSearch, Queries: []string{"q"}})
	if err != nil {
		t.Fatal(err)
	}
	correlation, err := NewToolCallID("toolu_swobu_5_0")
	if err != nil {
		t.Fatal(err)
	}

	// Idless replay: nil refinement, correlation stays internal.
	idless, err := NewToolCallItemWithResponsesWebSearch(correlation, WebSearchToolKey(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	idlessCall, _ := idless.ToolCall()
	if _, ok := idlessCall.ResponsesWebSearch(); ok {
		t.Fatal("idless web-search call must carry no Responses refinement")
	}
	if idlessCall.CallID() != correlation {
		t.Fatal("correlation token disturbed by absent refinement")
	}

	// Idful replay: refinement preserves the exact provider id verbatim.
	refinement, err := NewResponsesWebSearchRefinement(ResponsesItemID("ws_real_42"))
	if err != nil {
		t.Fatal(err)
	}
	idful, err := NewToolCallItemWithResponsesWebSearch(correlation, WebSearchToolKey(), input, &refinement)
	if err != nil {
		t.Fatal(err)
	}
	idfulCall, _ := idful.ToolCall()
	preserved, ok := idfulCall.ResponsesWebSearch()
	if !ok || preserved.ItemID().String() != "ws_real_42" {
		t.Fatalf("refinement = %#v preserved=%v", preserved, ok)
	}
	if idfulCall.CallID() != correlation {
		t.Fatal("refinement must not overwrite the canonical correlation token")
	}

	// Clone independence: the refinement survives a deep clone.
	if _, ok := idful.Clone().ToolCall(); !ok {
		t.Fatal("Clone dropped the tool call")
	}
	idfulCloneCall, _ := idful.ToolCall()
	preservedClone, ok := idfulCloneCall.ResponsesWebSearch()
	if !ok || preservedClone.ItemID().String() != "ws_real_42" {
		t.Fatalf("Clone dropped the Responses refinement: %#v ok=%v", preservedClone, ok)
	}
	if _, err := NewResponsesWebSearchRefinement(ResponsesItemID("")); err == nil {
		t.Fatal("blank refinement accepted")
	}
}
