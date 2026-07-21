package canonical

import (
	"testing"
)

func TestWebSearchCallPreservesOrderedDuplicateQueries(t *testing.T) {
	call := WebSearchCall{Action: WebSearchActionSearch, Queries: []string{"first", "first", "second"}}
	input, err := NewWebSearchToolInput(call)
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := NewToolCallID("search_1")
	item, err := NewToolCallItem(callID, WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := item.ToolCall()
	search, ok := got.Input().WebSearch()
	if !ok || len(search.Queries) != 3 || search.Queries[0] != search.Queries[1] {
		t.Fatalf("queries = %#v", search.Queries)
	}
	if _, err := NewToolCallItem(callID, WebSearchToolKey(), NewJSONObjectToolInput(JSONObject{})); err == nil {
		t.Fatal("web-search call accepted object input")
	}
}

func TestWebSearchCallRejectsIncompleteObservedActions(t *testing.T) {
	webURL, err := NewWebURL("https://example.com/source")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		call WebSearchCall
	}{
		{name: "open page without URL", call: WebSearchCall{Action: WebSearchActionOpenPage}},
		{name: "find in page without URL or match", call: WebSearchCall{Action: WebSearchActionFindInPage}},
		{name: "find in page without URL", call: WebSearchCall{Action: WebSearchActionFindInPage, Match: Specify("needle")}},
		{name: "find in page without match", call: WebSearchCall{Action: WebSearchActionFindInPage, URL: Specify(webURL)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call.Validate(); err == nil {
				t.Fatal("incomplete web-search action was accepted")
			}
		})
	}
}

func TestFunctionAndCustomCallsRejectWebSearchInput(t *testing.T) {
	callID, _ := NewToolCallID("call_1")
	input, err := NewWebSearchToolInput(WebSearchCall{Action: WebSearchActionSearch})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []ToolKind{ToolKindFunction, ToolKindCustom} {
		key, _ := NewRequestToolKey(kind, "tool")
		if _, err := NewToolCallItem(callID, key, input); err == nil {
			t.Fatalf("%s call accepted web-search input", kind)
		}
	}
}

func TestToolPolicySpecificRequiresEffectiveDeclaration(t *testing.T) {
	key := WebSearchToolKey()
	policy := NewToolPolicy(ToolPolicySpecific, &key)
	if err := policy.ValidateForTools(nil); err == nil {
		t.Fatal("specific policy accepted an empty effective tool set")
	}
	declaration := NewWebSearchDeclaration()
	if err := policy.ValidateForTools([]ToolDeclaration{declaration}); err != nil {
		t.Fatal(err)
	}
}

func TestWebSearchResultCorrelatesToPriorResponseCall(t *testing.T) {
	callID, _ := NewToolCallID("search_1")
	input, err := NewWebSearchToolInput(WebSearchCall{Action: WebSearchActionSearch})
	if err != nil {
		t.Fatal(err)
	}
	call, _ := NewToolCallItem(callID, WebSearchToolKey(), input)
	webURL, _ := NewWebURL("https://example.com/source")
	source, _ := NewWebSource(webURL, Specify("Example"))
	result, _ := NewWebSearchResult([]WebSource{source})
	resultItem, _ := NewWebSearchResultItem(callID, result)
	responseID := ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}
	if _, err := NewCanonicalResponse(responseID, "model", []CanonicalItem{call, resultItem}, "stop", NewUnknownTokenUsage()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCanonicalResponse(responseID, "model", []CanonicalItem{resultItem}, "stop", NewUnknownTokenUsage()); err == nil {
		t.Fatal("response accepted web-search result without prior call")
	}
}

func TestWebCitationValidatesUTF8ByteBoundaries(t *testing.T) {
	webURL, _ := NewWebURL("https://example.com")
	source, _ := NewWebSource(webURL, Unspecified[string]())
	text := "A£B"
	valid := WebCitation{Source: source, Excerpt: Specify("£"), Start: Specify(uint32(1)), End: Specify(uint32(3))}
	part, err := NewCitedTextMessagePart(text, []WebCitation{valid})
	if err != nil || len(part.Citations()) != 1 {
		t.Fatalf("valid citation = (%#v, %v)", part, err)
	}
	invalid := WebCitation{Source: source, Start: Specify(uint32(2)), End: Specify(uint32(3))}
	if _, err := NewCitedTextMessagePart(text, []WebCitation{invalid}); err == nil {
		t.Fatal("citation accepted an offset inside a UTF-8 rune")
	}
	if _, err := NewCitedTextMessagePart(text, []WebCitation{{Source: source, Excerpt: Specify(" ")}}); err == nil {
		t.Fatal("citation accepted an empty excerpt")
	}
}
