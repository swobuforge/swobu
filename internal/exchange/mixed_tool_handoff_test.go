package exchange

import (
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestMixedToolHandoffSettlesLocalHistoryAndResumesCallerResult(t *testing.T) {
	localKey, _ := canonical.NewToolKey("workspace/mcp", canonical.ToolKindFunction, "docs__search")
	clientKey, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "client_tool")
	localID, _ := canonical.NewToolCallID("call_local")
	clientID, _ := canonical.NewToolCallID("call_client")
	object, _ := canonical.ParseJSONObject([]byte(`{}`))
	localItem, _ := canonical.NewToolCallItem(localID, localKey, canonical.NewJSONObjectToolInput(object))
	clientItem, _ := canonical.NewToolCallItem(clientID, clientKey, canonical.NewJSONObjectToolInput(object))
	localCall, _ := localItem.ToolCall()
	localResult, _ := canonical.NewToolResultItem(localID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("local result")}, false)
	clientResult, _ := canonical.NewToolResultItem(clientID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("client result")}, false)
	searchID, _ := canonical.NewToolCallID("provider_search")
	searchInput, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	searchCall, err := canonical.NewToolCallItem(searchID, canonical.WebSearchToolKey(), searchInput)
	if err != nil {
		t.Fatal(err)
	}
	search, err := canonical.NewWebSearchResult(nil)
	if err != nil {
		t.Fatal(err)
	}
	searchResult, err := canonical.NewWebSearchResultItem(searchID, search)
	if err != nil {
		t.Fatal(err)
	}
	ordered := []canonical.CanonicalItem{testMessage(canonical.MessageRoleAssistant, "before"), clientItem, searchCall, testMessage(canonical.MessageRoleAssistant, "between"), localItem, searchResult, testMessage(canonical.MessageRoleAssistant, "after")}
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "swobu_mixed", Responses: &canonical.ResponsesContinuation{ProviderResponseID: "provider_round", TargetID: "target", TargetVersion: 1}}, "m", ordered, canonical.Completed("tool_calls"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := prepareMixedToolHandoff(testCanonicalRequest("m"), response, []canonical.ToolCallItem{localCall}, []canonical.CanonicalItem{localResult})
	if err != nil {
		t.Fatal(err)
	}
	if handoff.response.Response().Responses != nil {
		t.Fatal("handoff retained native authority")
	}
	visible := []canonical.CanonicalItem{ordered[0], clientItem, searchCall, ordered[3], searchResult, ordered[6]}
	if !reflect.DeepEqual(handoff.client.Items(), visible) {
		t.Fatal("client projection leaked local work or reordered visible content")
	}
	checkpoint := continuity.Checkpoint{Request: handoff.request, Response: handoff.response}
	store := continuity.NewMemoryStore()
	if err := store.Put(t.Context(), "mixed", continuity.Commit{Checkpoint: checkpoint}); err != nil {
		t.Fatal(err)
	}
	checkpoint, found, err := store.Get(t.Context(), "mixed", "swobu_mixed")
	if err != nil || !found {
		t.Fatalf("ordered checkpoint unavailable: found=%t err=%v", found, err)
	}
	current := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_mixed"}, Items: []canonical.CanonicalItem{clientResult}})
	resumed, err := continuity.Resume(current, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	want := append(testCanonicalRequest("m").Items(), ordered...)
	want = append(want, localResult, clientResult)
	if !reflect.DeepEqual(resumed.Request().Items(), want) {
		t.Fatal("mixed replay reordered original provider content")
	}
	wrong := current.WithItems([]canonical.CanonicalItem{localResult})
	if _, err := continuity.Resume(wrong, checkpoint); err == nil {
		t.Fatal("settled local result admitted as pending caller work")
	}
	if _, err := continuity.Resume(current.WithItems([]canonical.CanonicalItem{searchResult}), checkpoint); err == nil {
		t.Fatal("settled provider result admitted as pending caller work")
	}
	var matcher canonical.ToolEffectMatcher
	for index, item := range resumed.Request().Items() {
		if _, err := matcher.Accept(index, item); err != nil {
			t.Fatal(err)
		}
	}
	if len(matcher.Pending()) != 0 || resumed.HasPreviousHistory() {
		t.Fatal("resume duplicated local work or revived native omission authority")
	}
	// A branch rereads the immutable checkpoint, not the first resume's results.
	branch, err := continuity.Resume(current, checkpoint)
	if err != nil || !reflect.DeepEqual(branch.Request().Items(), want) {
		t.Fatalf("mixed checkpoint branch changed ordered history: %v", err)
	}
	for name, results := range map[string][]canonical.CanonicalItem{"missing": nil, "foreign": {clientResult}, "duplicate": {localResult, localResult}} {
		t.Run(name, func(t *testing.T) {
			if _, err := prepareMixedToolHandoff(testCanonicalRequest("m"), response, []canonical.ToolCallItem{localCall}, results); err == nil {
				t.Fatal("invalid result batch accepted")
			}
		})
	}
}
