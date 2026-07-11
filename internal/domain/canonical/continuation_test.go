package canonical

import "testing"

func TestTurnRef_NormalizesAndClonesPreviousID(t *testing.T) {
	ref := NewTurnRef("  resp_123  ")
	if ref.IsZero() {
		t.Fatal("IsZero() = true, want false")
	}
	prev, ok := ref.PreviousID()
	if !ok {
		t.Fatal("PreviousID() = false, want true")
	}
	if got := prev.String(); got != "resp_123" {
		t.Fatalf("PreviousID() = %q, want %q", got, "resp_123")
	}
	cloned := ref.Clone()
	if cloned.Previous == nil || cloned.Previous == ref.Previous {
		t.Fatal("Clone() must deep-copy the turn reference")
	}
}

func TestCurrentTurnDelta_ReturnsSuffixStartingAtLatestUserItem(t *testing.T) {
	delta := CurrentTurnDelta([]CanonicalItem{
		NewTextItem(ItemAuthorUser, "hi"),
		NewTextItem(ItemAuthorAssistant, "hello"),
		NewTextItem(ItemAuthorUser, "continue"),
	})
	if len(delta) != 1 {
		t.Fatalf("delta len = %d, want 1", len(delta))
	}
	if got := delta[0].Text; got != "continue" {
		t.Fatalf("delta[0].Text = %q, want %q", got, "continue")
	}
}

func TestCurrentTurnDelta_FallsBackToCloneWhenNoUserItemExists(t *testing.T) {
	original := []CanonicalItem{NewTextItem(ItemAuthorAssistant, "hello")}
	delta := CurrentTurnDelta(original)
	if len(delta) != 1 || delta[0].Text != "hello" {
		t.Fatalf("delta = %+v, want clone of original", delta)
	}
	delta[0].Text = "mutated"
	if original[0].Text != "hello" {
		t.Fatal("CurrentTurnDelta must return a clone")
	}
}

func TestContinuationRecord_CloneCopiesPointerFieldsAndPayloads(t *testing.T) {
	parent := NewContinuationID("resp_parent")
	record := ContinuationRecord{
		ID:           NewContinuationID("resp_child"),
		Parent:       &parent,
		RouteID:      "alpha",
		ModelID:      "m",
		RequestDelta: NewCanonicalRequest(RequestParams{Model: "m", Items: []CanonicalItem{NewTextItem(ItemAuthorUser, "hi")}}),
		Response: NewConversationOutput(
			"resp_child",
			"m",
			[]OutputItem{NewTextOutputItem("text_0", "done")},
			"completed",
		),
	}

	cloned := record.Clone()
	if cloned.Parent == nil || cloned.Parent == record.Parent || cloned.Parent.String() != "resp_parent" {
		t.Fatal("Clone() must deep-copy the parent pointer")
	}
	if cloned.RequestDelta.Items()[0].Text != "hi" {
		t.Fatal("Clone() lost request delta")
	}
	if cloned.Response.ResultID() != "resp_child" {
		t.Fatal("Clone() lost response snapshot")
	}
}
