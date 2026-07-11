package canonical

import "testing"

func TestContinuationSelectorFromRequest_AcceptsPreviousResponseID(t *testing.T) {
	_, ok := ContinuationSelectorFromRequest(NewCanonicalRequest(RequestParams{
		Model:              "m",
		PreviousResponseID: "resp_1",
	}))
	if !ok {
		t.Fatal("ok = false, want true")
	}
}

func TestContinuationSelectorFromRequest_MissingSelectorReturnsFalse(t *testing.T) {
	_, ok := ContinuationSelectorFromRequest(NewCanonicalRequest(RequestParams{
		Model: "m",
	}))
	if ok {
		t.Fatal("ok = true, want false")
	}
}

func TestContinuationConversation_RehydratesResponseConversation(t *testing.T) {
	conversation, ok, err := ContinuationConversation(NewCanonicalRequest(RequestParams{
		Model: "m",
		Items: []CanonicalItem{
			NewTextItem(ItemAuthorUser, "hi"),
			NewTextItem(ItemAuthorAssistant, "hello"),
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
				NewToolUseOutputItem("tool_0", "call_1", "grep", NewToolArgumentsObject(`{"pattern":"TODO"}`)),
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
