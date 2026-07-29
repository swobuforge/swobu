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

func mustReasoningPart(t *testing.T, kind ReasoningPartKind, text string) ReasoningPart {
	t.Helper()
	part, err := NewReasoningPart(kind, text)
	if err != nil {
		t.Fatal(err)
	}
	return part
}
