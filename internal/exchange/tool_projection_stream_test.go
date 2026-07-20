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
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Tools: canonical.Specify(set)})
	projected, table, _, err := provider.ProjectAttemptTools(request)
	if err != nil {
		t.Fatal(err)
	}
	attemptKey := projected.Tools()[0].Key()
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
