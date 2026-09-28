package exchange

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
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

func TestMCPPreflightFailureDoesNotCreateExecution(t *testing.T) {
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
	_, err = reduceCallingMCP(
		context.Background(), exchangeState{mcp: &mcp.Run{}},
		callingMCPPhase{calls: []canonical.ToolCallItem{call}},
		mcpBatchStarted{},
		runtimeBundle{},
	)
	if err == nil {
		t.Fatal("MCP preflight without a binding unexpectedly reached execution")
	}
}

func TestCancelledMCPBatchDoesNotBeginPreflightOrConsumeOrdinal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ordinal := uint64(0)
	state := exchangeState{mcp: &mcp.Run{}, invocationOrdinal: &ordinal}
	outcome, err := reduceCallingMCP(ctx, state, callingMCPPhase{calls: []canonical.ToolCallItem{{}}}, mcpBatchStarted{}, runtimeBundle{})
	if err != nil || ordinal != 0 || outcome.command != nil {
		t.Fatalf("pre-cancelled MCP: err=%v ordinal=%d command=%T", err, ordinal, outcome.command)
	}
}

func TestMCPExecuteCancellationSettlesWithoutFailureSubtype(t *testing.T) {
	id, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	invocationID, err := execution.NewInvocationID(id, 1)
	if err != nil {
		t.Fatal(err)
	}
	capture := &characterizationEvidenceCapture{}
	state := exchangeState{}
	phase := callingMCPPhase{invocationID: invocationID}
	outcome, err := reduceCallingMCP(context.Background(), state, phase, mcpToolReturned{err: context.Canceled}, runtimeBundle{ExecutionEvidence: capture})
	if err != nil || len(capture.events) != 1 {
		t.Fatalf("MCP cancellation: transition=%#v err=%v events=%v", outcome, err, capture.events)
	}
	settled := capture.events[0].(executionevidence.MCPInvocationSettled)
	if settled.Outcome() != execution.Cancelled || settled.Occurrence() != execution.MayHaveOccurred || settled.Failure() != "" {
		t.Fatalf("MCP cancellation settlement = %#v", settled)
	}
	if _, ok := outcome.nextState.phase.(failedPhase); !ok {
		t.Fatalf("MCP cancellation did not terminate exchange: %T", outcome.nextState.phase)
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

func TestClassifyMCPResultUsesToolErrorBitNotItemKindBit(t *testing.T) {
	callID, err := canonical.NewToolCallID("call_classify")
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("ok")}, false)
	if err != nil {
		t.Fatal(err)
	}
	outcome, failure, ok := classifyMCPResult(result)
	if !ok || outcome != execution.Succeeded || failure != "" {
		t.Fatalf("ordinary MCP result classified as outcome=%q failure=%q ok=%v", outcome, failure, ok)
	}
}

func TestClassifyMCPResultPreservesRemoteToolError(t *testing.T) {
	callID, err := canonical.NewToolCallID("call_error")
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("denied")}, true)
	if err != nil {
		t.Fatal(err)
	}
	outcome, failure, ok := classifyMCPResult(result)
	if !ok || outcome != execution.Failed || failure != executionevidence.FailureToolError {
		t.Fatalf("remote MCP error classified as outcome=%q failure=%q ok=%v", outcome, failure, ok)
	}
}
