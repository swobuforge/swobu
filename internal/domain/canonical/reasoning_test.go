package canonical

import (
	"fmt"
	"strings"
	"testing"
)

func TestReasoningComputeClosedStates(t *testing.T) {
	if NewDisabledReasoningCompute().Kind() != ReasoningDisabled {
		t.Fatal("disabled compute lost its branch")
	}
	if NewAutomaticReasoningCompute().Kind() != ReasoningAutomatic {
		t.Fatal("automatic compute lost its branch")
	}
	budget, err := NewBudgetReasoningCompute(4000)
	if err != nil {
		t.Fatal(err)
	}
	if tokens, ok := budget.Tokens(); !ok || tokens != 4000 {
		t.Fatalf("budget = %d, %v", tokens, ok)
	}
	if _, err := NewBudgetReasoningCompute(0); err == nil {
		t.Fatal("non-positive budget was accepted")
	}
}

func TestDisabledReasoningConflictsWithSummaryDisclosure(t *testing.T) {
	_, err := NewReasoningControls(ReasoningControlsParams{
		Compute:    Specify(NewDisabledReasoningCompute()),
		Disclosure: Specify(ReasoningDisclosureSummary),
	})
	if err == nil {
		t.Fatal("disabled reasoning accepted summary disclosure")
	}
}

func TestReasoningControlsPreserveClosedResponsesContextPresence(t *testing.T) {
	for _, value := range []ResponsesReasoningContext{
		ResponsesReasoningContextAuto,
		ResponsesReasoningContextAllTurns,
		ResponsesReasoningContextCurrentTurn,
	} {
		controls, err := NewReasoningControls(ReasoningControlsParams{ResponsesContext: Specify(value)})
		if err != nil {
			t.Fatalf("%q rejected: %v", value, err)
		}
		got, present := controls.Clone().ResponsesContextField().Get()
		if !present || got != value {
			t.Fatalf("cloned context = (%q,%t), want (%q,true)", got, present, value)
		}
	}
	omitted, err := NewReasoningControls(ReasoningControlsParams{
		ResponsesContext: Unspecified[ResponsesReasoningContext](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if omitted.ResponsesContextField().IsSpecified() {
		t.Fatal("omitted Responses context became specified")
	}
	if _, err := NewReasoningControls(ReasoningControlsParams{
		ResponsesContext: Specify(ResponsesReasoningContext("future")),
	}); err == nil {
		t.Fatal("unknown Responses reasoning context was accepted")
	}
}

func TestReasoningPartsAndOpaqueThinkingRemainDistinct(t *testing.T) {
	summary, _ := NewReasoningPart(ReasoningPartSummary, "summary")
	trace, _ := NewReasoningPart(ReasoningPartTrace, "trace")
	raw := []byte(`{"type":"redacted_thinking","data":"secret"}`)
	native, err := NewMessagesOpaqueThinking(raw)
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewReasoningItem([]ReasoningPart{summary, trace}, native)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'x'
	reasoning, _ := item.Reasoning()
	parts := reasoning.Parts()
	if parts[0].Kind() != ReasoningPartSummary || parts[1].Kind() != ReasoningPartTrace {
		t.Fatalf("parts = %#v", parts)
	}
	got, ok := reasoning.Opaque().Messages()
	if !ok || got[0] != '{' {
		t.Fatalf("opaque branch was not cloned: %q", got)
	}
	if strings.Contains(fmt.Sprintf("%#v", reasoning.Opaque()), "secret") || strings.Contains(fmt.Sprint(reasoning.Opaque()), "secret") {
		t.Fatal("opaque thinking leaked through formatting")
	}
}

func TestInteractionsOpaqueThinkingIsClosedAndDefensivelyCopied(t *testing.T) {
	raw := []byte(`{"type":"thought","signature":"secret"}`)
	opaque, err := NewInteractionsOpaqueThinking(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'x'
	got, ok := opaque.Clone().Interactions()
	if !ok || string(got) != `{"type":"thought","signature":"secret"}` {
		t.Fatalf("Interactions replay = %q/%t", got, ok)
	}
	if _, foreign := opaque.Messages(); foreign {
		t.Fatal("Interactions replay escaped through Messages branch")
	}
	if _, err := NewInteractionsOpaqueThinking(nil); err == nil {
		t.Fatal("empty Interactions replay was accepted")
	}
}

func TestReasoningIsAssistantOwnedAndAtomicInSynthesizedStream(t *testing.T) {
	part, _ := NewReasoningPart(ReasoningPartSummary, "summary")
	item, _ := NewReasoningItem([]ReasoningPart{part}, OpaqueThinking{})
	if item.Owner() != TurnOwnerAssistant {
		t.Fatalf("owner = %q", item.Owner())
	}
	events := SynthesizeResponseEnvelopeEvents("ex", ResponseRef{SwobuID: "resp"}, "model", []CanonicalItem{item}, Completed("stop"), TokenUsage{})
	for _, event := range events {
		if event.Kind == EventItemStart || event.Kind == EventContentStart || event.Kind == EventTextDelta {
			t.Fatalf("reasoning emitted progressive event %q", event.Kind)
		}
	}
}

func TestResponsesReasoningReplayPreservesOptionalItemID(t *testing.T) {
	// RFC G2 §7.1 / §11.1: encrypted content is required; the paired Responses
	// wire id is optional and preserved verbatim through construction, readback,
	// and clone. Idless replay stays idless; ciphertext never reaches string forms.
	withID, err := NewResponsesOpaqueThinking(ResponsesReasoningReplay{EncryptedContent: "cipher", ItemID: "rs_1"})
	if err != nil {
		t.Fatalf("with-id construct: %v", err)
	}
	replay, ok := withID.Responses()
	if !ok || replay.EncryptedContent != "cipher" || replay.ItemID != "rs_1" {
		t.Fatalf("readback = (%q,%q,%t), want (cipher,rs_1,true)", replay.EncryptedContent, replay.ItemID, ok)
	}
	cloned, ok := withID.Clone().Responses()
	if !ok || cloned != replay {
		t.Fatalf("clone did not copy both fields independently: %+v", cloned)
	}

	idless, err := NewResponsesOpaqueThinking(ResponsesReasoningReplay{EncryptedContent: "cipher"})
	if err != nil {
		t.Fatalf("idless construct: %v", err)
	}
	if replay, ok := idless.Responses(); !ok || replay.ItemID != "" {
		t.Fatalf("idless replay gained an id: %+v", replay)
	}

	if _, err := NewResponsesOpaqueThinking(ResponsesReasoningReplay{ItemID: "rs_1"}); err == nil {
		t.Fatal("empty encrypted content was accepted")
	}

	// Only the Responses branch may carry a wire id; Messages and provider Chat
	// branches must never silently absorb one (validate is the construction
	// invariant guard).
	messages, err := NewMessagesOpaqueThinking([]byte(`{"x":"y"}`))
	if err != nil {
		t.Fatal(err)
	}
	if replay, ok := messages.Responses(); ok {
		t.Fatalf("messages branch exposed a responses replay: %+v", replay)
	}

	for _, formatted := range []string{fmt.Sprint(withID), fmt.Sprintf("%#v", withID)} {
		if strings.Contains(formatted, "cipher") || strings.Contains(formatted, "rs_1") {
			t.Fatalf("opaque thinking leaked through formatting: %q", formatted)
		}
	}
}

func TestProviderChatOpaqueThinkingIsScopedAndDefensivelyCopied(t *testing.T) {
	const (
		ownerScope ProviderChatReplayScope = "owner-chat-replay"
		otherScope ProviderChatReplayScope = "other-chat-replay"
	)

	raw := []byte("opaque provider reasoning")
	opaque, err := NewProviderChatOpaqueThinking(ownerScope, raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'x'

	if _, ok := opaque.ProviderChat(otherScope); ok {
		t.Fatal("foreign provider Chat scope exposed opaque replay bytes")
	}
	got, ok := opaque.ProviderChat(ownerScope)
	if !ok || string(got) != "opaque provider reasoning" {
		t.Fatalf("same-scope replay = %q, %t", got, ok)
	}
	got[0] = 'x'
	cloned, ok := opaque.Clone().ProviderChat(ownerScope)
	if !ok || string(cloned) != "opaque provider reasoning" {
		t.Fatalf("clone replay = %q, %t", cloned, ok)
	}

	if _, err := NewProviderChatOpaqueThinking("", []byte("opaque")); err == nil {
		t.Fatal("empty provider Chat scope was accepted")
	}
	if _, err := NewProviderChatOpaqueThinking(ownerScope, nil); err == nil {
		t.Fatal("empty provider Chat replay bytes were accepted")
	}
	invalid := OpaqueThinking{kind: opaqueThinkingProviderChat, raw: []byte("opaque")}
	if _, err := NewReasoningItem(nil, invalid); err == nil {
		t.Fatal("provider Chat branch without a scope was accepted")
	}
	invalid = OpaqueThinking{providerChatScope: ownerScope}
	if invalid.IsZero() {
		t.Fatal("partially populated provider Chat state was treated as zero")
	}
	if _, err := NewReasoningItem(nil, invalid); err == nil {
		t.Fatal("scope-only opaque thinking was accepted")
	}
}

func mustReasoningPart(t *testing.T, kind ReasoningPartKind, text string) ReasoningPart {
	t.Helper()
	part, err := NewReasoningPart(kind, text)
	if err != nil {
		t.Fatal(err)
	}
	return part
}

func TestOpaqueThinkingTargetOriginProvenance(t *testing.T) {
	unbound, err := NewResponsesOpaqueThinking(ResponsesReasoningReplay{EncryptedContent: "cipher-token", ItemID: "rs_orig"})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Unbound origin
	if unbound.MatchesTarget("target-a", 1) {
		t.Fatal("unbound opaque thinking matched target-a:1")
	}

	// 2. Invalid origin binding parameters
	if _, err := unbound.withTargetOrigin("", 1); err == nil {
		t.Fatal("empty target ID accepted for origin binding")
	}
	if _, err := unbound.withTargetOrigin("target-a", 0); err == nil {
		t.Fatal("zero target version accepted for origin binding")
	}

	// 3. Provider binding
	bound, err := unbound.withTargetOrigin("target-a", 1)
	if err != nil {
		t.Fatalf("withTargetOrigin failed: %v", err)
	}
	if !bound.MatchesTarget("target-a", 1) {
		t.Fatal("bound opaque thinking did not match target-a:1")
	}
	if bound.MatchesTarget("target-a", 2) {
		t.Fatal("bound opaque thinking matched different target version")
	}
	if bound.MatchesTarget("target-b", 1) {
		t.Fatal("bound opaque thinking matched different target ID")
	}

	// 4. Same binding is stable
	reboundSame, err := bound.withTargetOrigin("target-a", 1)
	if err != nil {
		t.Fatalf("rebinding to same target failed: %v", err)
	}
	if !reboundSame.MatchesTarget("target-a", 1) {
		t.Fatal("rebound same did not match target-a:1")
	}

	// 5. Different rebinding is rejected
	if _, err := bound.withTargetOrigin("target-b", 1); err == nil {
		t.Fatal("rebinding to different target ID was accepted")
	}
	if _, err := bound.withTargetOrigin("target-a", 2); err == nil {
		t.Fatal("rebinding to different target version was accepted")
	}

	// 6. Clone preserves origin
	cloned := bound.Clone()
	if !cloned.MatchesTarget("target-a", 1) {
		t.Fatal("cloned bound opaque thinking did not match target-a:1")
	}

	// 7. Zero value is safe
	zero := OpaqueThinking{}
	if zeroBound, err := zero.withTargetOrigin("target-a", 1); err != nil || !zeroBound.IsZero() {
		t.Fatalf("zero withTargetOrigin = (%v, %v), want (zero, nil)", zeroBound, err)
	}
	if zero.MatchesTarget("target-a", 1) {
		t.Fatal("zero MatchesTarget returned true")
	}
}

func TestBoundResponseIdentityStreamBindsReasoningItem(t *testing.T) {
	unbound, err := NewResponsesOpaqueThinking(ResponsesReasoningReplay{EncryptedContent: "enc_data", ItemID: "rs_123"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewReasoningItem([]ReasoningPart{mustReasoningPart(t, ReasoningPartSummary, "thinking summary")}, unbound)
	if err != nil {
		t.Fatal(err)
	}

	stream := NewSliceEventReader([]Event{
		{Kind: EventResponseIdentity, Payload: ResponseIdentityPayload{Response: ResponseRef{SwobuID: "resp_native"}}},
		{Kind: EventItemCompleted, Payload: ItemEvent{Position: ItemPosition{Item: 0}, Payload: ItemCompletedPayload{Item: item}}},
	})

	boundStream := NewBoundResponseIdentityStream(stream, ResponseBinding{
		SwobuID:       "resp_swobu",
		TargetID:      "target-provider-x",
		TargetVersion: 3,
	})

	ev1, err := boundStream.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	if ev1.Kind != EventResponseIdentity {
		t.Fatalf("first event = %v, want EventResponseIdentity", ev1.Kind)
	}

	ev2, err := boundStream.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	if ev2.Kind != EventItemCompleted {
		t.Fatalf("second event = %v, want EventItemCompleted", ev2.Kind)
	}

	completed := ev2.Payload.(ItemEvent).Payload.(ItemCompletedPayload)
	reasoning, ok := completed.Item.Reasoning()
	if !ok {
		t.Fatal("completed item was not reasoning")
	}
	if !reasoning.Opaque().MatchesTarget("target-provider-x", 3) {
		t.Fatal("completed reasoning item was not bound to target-provider-x:3")
	}
	if reasoning.Opaque().MatchesTarget("target-provider-x", 2) {
		t.Fatal("completed reasoning item matched target-provider-x:2")
	}
}
