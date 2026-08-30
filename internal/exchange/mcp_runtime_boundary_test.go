package exchange

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
)

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
