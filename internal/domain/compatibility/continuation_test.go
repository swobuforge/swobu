package compatibility

import "testing"

func TestPreviousResponseIDFromRequest_RejectsBothSelectors(t *testing.T) {
	_, ok, err := PreviousResponseIDFromRequest(NewGenerationRequest(GenerationRequestParams{
		Model:              "m",
		PreviousResponseID: "resp_1",
		ConversationID:     "conv_1",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if ok {
		t.Fatal("ok = true, want false")
	}
}

func TestPreviousResponseIDFromRequest_RejectsConversation(t *testing.T) {
	_, ok, err := PreviousResponseIDFromRequest(NewGenerationRequest(GenerationRequestParams{
		Model:          "m",
		ConversationID: "conv_1",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if ok {
		t.Fatal("ok = true, want false")
	}
}

func TestContinuationConversation_RehydratesResponseConversation(t *testing.T) {
	conversation, ok, err := ContinuationConversation(NewGenerationRequest(GenerationRequestParams{
		Model: "m",
		Thread: []CanonicalItem{
			NewTextItem(ItemAuthorUser, "hi"),
			NewTextItem(ItemAuthorAssistant, "hello"),
			NewTextItem(ItemAuthorUser, "continue"),
		},
		LastTurn: []CanonicalItem{
			NewTextItem(ItemAuthorUser, "continue"),
		},
	}))
	if err != nil {
		t.Fatalf("ContinuationConversation returned error: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if len(conversation) != 3 {
		t.Fatalf("conversation len = %d, want 3", len(conversation))
	}
	if got := conversation[2].Text; got != "continue" {
		t.Fatalf("latest text = %q, want %q", got, "continue")
	}
}

func TestBuildContinuitySnapshot_AppendsAssistantOutput(t *testing.T) {
	snapshot, ok, err := BuildContinuitySnapshot(
		[]CanonicalItem{NewTextItem(ItemAuthorUser, "hi")},
		NewConversationOutput(
			"resp_1", "m",
			[]OutputItem{
				NewTextOutputItem("text_0", "hello"),
				NewToolUseOutputItem("tool_0", "call_1", "grep", map[string]any{"pattern": "TODO"}),
			},
			"completed",
		),
	)
	if err != nil {
		t.Fatalf("BuildContinuitySnapshot returned error: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got := snapshot.ResponseID; got != "resp_1" {
		t.Fatalf("response id = %q, want %q", got, "resp_1")
	}
	if len(snapshot.Thread) != 3 {
		t.Fatalf("thread len = %d, want 3", len(snapshot.Thread))
	}
}
