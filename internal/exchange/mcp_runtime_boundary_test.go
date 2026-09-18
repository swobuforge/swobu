package exchange

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
)

func TestLocalContinuationConsumesOpaqueProviderViewOnly(t *testing.T) {
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("use the tool")})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := continuity.Begin(canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{message}}))
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := canonical.NewToolCallID("call_1")
	tool, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
	input, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, _ := canonical.NewToolCallItem(callID, tool, canonical.NewJSONObjectToolInput(input))
	client, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "resp_1"}, "model", []canonical.CanonicalItem{call}, canonical.Completed("tool_calls"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	details := json.RawMessage(`[{"type":"reasoning.signature","signature":"opaque"}]`)
	opaque, err := canonical.NewProviderChatOpaqueThinking("openrouter-chat", details)
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem(nil, opaque)
	if err != nil {
		t.Fatal(err)
	}
	continuation, err := client.WithReasoningPrelude(reasoning)
	if err != nil {
		t.Fatal(err)
	}
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)

	continued, err := continueAfterLocalResults(prepared, completedProviderResponse{client: client, continuation: continuation}, []canonical.CanonicalItem{result})
	if err != nil {
		t.Fatal(err)
	}
	items := continued.Request().Items()
	if len(items) != 4 {
		t.Fatalf("continued items = %d, want prompt + opaque reasoning + call + result", len(items))
	}
	if _, ok := client.Items()[0].Reasoning(); ok || len(client.Items()) != 1 {
		t.Fatal("checkpoint-only reasoning leaked into client response")
	}
	if value, ok := items[1].Reasoning(); !ok || value.Opaque().IsZero() {
		t.Fatal("local continuation dropped opaque provider reasoning")
	}
}

func TestUnavailableOnlyMCPRuntimeDoesNotDelayClientHandoff(t *testing.T) {
	t.Parallel()
	delayed, err := delayClientHandoffFor(&mcp.Run{}, canonical.CanonicalRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if delayed {
		t.Fatal("runtime without executable bindings delayed incremental client handoff")
	}
}

func TestMCPExecutionRecordsPolyfillTruth(t *testing.T) {
	t.Parallel()
	callID, err := canonical.NewToolCallID("call_1")
	if err != nil {
		t.Fatal(err)
	}
	tool, err := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
	if err != nil {
		t.Fatal(err)
	}
	input, err := canonical.ParseJSONObject([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewToolCallItem(callID, tool, canonical.NewJSONObjectToolInput(input))
	if err != nil {
		t.Fatal(err)
	}
	call, _ := item.ToolCall()
	outcome, err := reduceCallingMCP(
		context.Background(), exchangeState{mcp: &mcp.Run{}},
		callingMCPPhase{calls: []canonical.ToolCallItem{call}},
		mcpBatchStarted{},
		runtimeBundle{},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Starting the local execution mechanism must not create compatibility
	// evidence; only semantic loss in a later projection may do that.
	if len(outcome.nextState.effectiveChanges) != 0 {
		t.Fatalf("MCP start changes = %#v, want exact", outcome.nextState.effectiveChanges)
	}
	if len(outcome.nextState.effectiveChanges) != 0 {
		t.Fatalf("MCP execution invented semantic changes = %#v", outcome.nextState.effectiveChanges)
	}
}

func TestMCPBatchReservationIsACommandBoundary(t *testing.T) {
	t.Parallel()
	outcome, err := beginMCPBatch(exchangeState{mcp: &mcp.Run{}}, callingMCPPhase{
		calls: []canonical.ToolCallItem{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outcome.command.(beginMCPBatchCommand); !ok {
		t.Fatalf("batch reservation command = %T", outcome.command)
	}
}
