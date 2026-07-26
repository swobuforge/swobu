package exchange

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestProviderToolProjectionStreamRestoresExactCanonicalKey(t *testing.T) {
	original, _ := canonical.NewToolKey("mcp/github/issues", canonical.ToolKindFunction, "create_issue")
	schema, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	declaration, _ := canonical.NewFunctionTool(original, "", canonical.NewToolSchemaObject(schema), canonical.Unspecified[bool]())
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	declarations, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarations}})
	projected, table, _, err := provider.ProjectAttemptTools(request)
	if err != nil {
		t.Fatal(err)
	}
	attemptTools, _ := canonical.ToolEnvironmentAt(projected.Items(), len(projected.Items()))
	attemptKey := attemptTools.Declarations()[0].Key()
	callID, _ := canonical.NewToolCallID("call_1")
	input, _ := canonical.ParseJSONObject([]byte(`{"title":"bug"}`))
	completed, _ := canonical.NewToolCallItem(callID, attemptKey, canonical.NewJSONObjectToolInput(input))
	start, _ := canonical.NewToolCallStart(callID, attemptKey)
	events := []canonical.Event{
		{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: start}},
		{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonical.ItemCompletedPayload{Item: completed}}},
	}
	stream := newCanonicalToolProjectionStream(canonical.NewSliceEventReader(events), table)
	started, err := stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	startEvent := started.Payload.(canonical.ItemEvent).Payload.(canonical.ItemStartPayload)
	startCall, _ := startEvent.ToolCall()
	if startCall.Tool != original {
		t.Fatalf("start key = %q, want %q", startCall.Tool, original)
	}
	finished, err := stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	finishedCall, _ := finished.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item.ToolCall()
	if finishedCall.Tool() != original {
		t.Fatalf("completed key = %q, want %q", finishedCall.Tool(), original)
	}
}

func TestProviderToolProjectionStreamPreservesWebSearchLifecycle(t *testing.T) {
	declaration := canonical.NewWebSearchDeclaration()
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	if err != nil {
		t.Fatal(err)
	}
	declarations, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarations}})
	_, table, _, err := provider.ProjectAttemptTools(request)
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := canonical.NewToolCallID("search_1")
	input, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{
		Action:  canonical.WebSearchActionSearch,
		Queries: []string{"deadline"},
	})
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	searchResult, err := canonical.NewWebSearchResult(nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewWebSearchResultItem(callID, searchResult)
	if err != nil {
		t.Fatal(err)
	}
	events := []canonical.Event{
		{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonical.ItemCompletedPayload{Item: call}}},
		{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 1}, Payload: canonical.ItemCompletedPayload{Item: result}}},
	}
	stream := newCanonicalToolProjectionStream(canonical.NewSliceEventReader(events), table)

	completedCall, err := stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	projectedCall, _ := completedCall.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item.ToolCall()
	if projectedCall.Tool() != canonical.WebSearchToolKey() || projectedCall.CallID() != callID {
		t.Fatalf("web-search call = %#v", projectedCall)
	}
	completedResult, err := stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	projectedResult, _ := completedResult.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item.ToolResult()
	if projectedResult.CallID() != callID {
		t.Fatalf("web-search result call ID = %q, want %q", projectedResult.CallID(), callID)
	}
}
