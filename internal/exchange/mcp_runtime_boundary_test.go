package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
)

func TestUnavailableOnlyMCPRuntimeDoesNotDelayClientHandoff(t *testing.T) {
	t.Parallel()
	if delayClientHandoffFor(&mcp.Run{}) {
		t.Fatal("runtime without executable bindings delayed incremental client handoff")
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
