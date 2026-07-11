package canonical

import "testing"

func TestConversationRequest_ClonesStructuredMessagesDeeply(t *testing.T) {
	req := NewCanonicalRequest(RequestParams{
		Model: "m",
		Items: []CanonicalItem{
			NewTextItem(ItemAuthorAssistant, "hi"),
			NewToolUseItem(ItemAuthorAssistant, "", "toolu_1", "calculator", map[string]any{"expr": "2+2"}),
		},
	})

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
	req := NewCanonicalRequest(RequestParams{
		Model:              "m",
		PreviousResponseID: "resp_123",
		CacheIntent: NewCacheIntent(CacheIntentParams{
			Key: "repo-alpha",
		}),
		Items: []CanonicalItem{
			NewToolUseItem(ItemAuthorAssistant, "", "call_1", "grep", map[string]any{"pattern": "TODO"}),
		},
	})

	cloned := req.Clone()
	items := cloned.Items()
	items[0].Input["pattern"] = "changed"

	got := req.Items()
	if got[0].Input["pattern"] != "TODO" {
		t.Fatalf("tool input = %v, want %q", got[0].Input["pattern"], "TODO")
	}
	if cloned.PreviousResponseID() != "resp_123" || cloned.CacheIntent().Key() != "repo-alpha" {
		t.Fatalf("clone lost response state")
	}
}
