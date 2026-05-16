package canonical

import "testing"

func TestConversationRequest_ClonesStructuredMessagesDeeply(t *testing.T) {
	req := NewDialogRequest(
		"m",
		[]CanonicalItem{
			NewTextItem(ItemAuthorAssistant, "hi"),
			NewToolUseItem(ItemAuthorAssistant, "", "toolu_1", "calculator", map[string]any{"expr": "2+2"}),
		},
	)

	cloned := req.Items()
	cloned[0].Text = "changed"
	cloned[1].Input["expr"] = "changed"

	got := req.Items()
	if got[0].Text != "hi" {
		t.Fatalf("text = %q, want %q", got[0].Text, "hi")
	}
	if got[1].Input["expr"] != "2+2" {
		t.Fatalf("tool input = %v, want %q", got[1].Input["expr"], "2+2")
	}
}

func TestResponseRequest_ClonesStructuredConversationStateDeeply(t *testing.T) {
	req := NewGenerationRequest(GenerationRequestParams{
		Model:              "m",
		PreviousResponseID: "resp_123",
		PromptCacheKey:     "repo-alpha",
		Items: []CanonicalItem{
			NewToolUseItem(ItemAuthorAssistant, "", "call_1", "grep", map[string]any{"pattern": "TODO"}),
		},
	})

	cloned, ok := req.Clone().(GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("clone type = %T, want GenerationCanonicalRequest", req.Clone())
	}
	items := cloned.Thread()
	items[0].Input["pattern"] = "changed"

	got := req.Thread()
	if got[0].Input["pattern"] != "TODO" {
		t.Fatalf("tool input = %v, want %q", got[0].Input["pattern"], "TODO")
	}
	if cloned.PreviousResponseID() != "resp_123" || cloned.PromptCacheKey() != "repo-alpha" {
		t.Fatalf("clone lost response state")
	}
}
