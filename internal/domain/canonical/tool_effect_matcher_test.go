package canonical

import "testing"

func TestToolEffectMatcherMatchesOccurrencesAndAllowsCompletedIDReuse(t *testing.T) {
	id, _ := NewToolCallID("call_reused")
	tool, _ := NewToolKey(ToolNamespaceRequest, ToolKindFunction, "lookup")
	input := NewJSONObjectToolInput(mustValidationObject(t, `{"q":"one"}`))
	firstCall, _ := NewToolCallItem(id, tool, input)
	firstResult, _ := NewToolResultItem(id, []ToolResultPart{NewTextToolResultPart("one")}, false)
	secondCall, _ := NewToolCallItem(id, tool, input)

	effects, err := MatchToolEffects([]CanonicalItem{firstCall, firstResult, secondCall})
	if err != nil {
		t.Fatal(err)
	}
	if len(effects) != 2 ||
		effects[0].CallIndex != 0 || effects[0].ResultIndex != 1 ||
		effects[1].CallIndex != 2 || effects[1].ResultIndex != -1 {
		t.Fatalf("effects = %#v", effects)
	}
}

func TestToolEffectMatcherRejectsMismatchedAndOrphanResults(t *testing.T) {
	id, _ := NewToolCallID("call_search")
	input, _ := NewWebSearchToolInput(WebSearchCall{Action: WebSearchActionSearch, Queries: []string{"swobu"}})
	call, _ := NewToolCallItem(id, WebSearchToolKey(), input)
	contentResult, _ := NewToolResultItem(id, []ToolResultPart{NewTextToolResultPart("not search evidence")}, false)
	if _, err := MatchToolEffects([]CanonicalItem{call, contentResult}); err == nil {
		t.Fatal("content result consumed a web-search call")
	}
	if _, err := MatchToolEffects([]CanonicalItem{contentResult}); err == nil {
		t.Fatal("orphan result was accepted")
	}
}
