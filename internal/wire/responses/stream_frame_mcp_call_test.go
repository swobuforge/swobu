package responses

import (
	"context"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"testing"
)

func TestResponsesEventReaderRejectsMCPStreamFrames(t *testing.T) {
	s := &responsesResponseStream{exchangeID: "ex", responseEnvID: "r", toolStates: map[string]responsesToolState{}, toolInputs: map[string]string{}, latestUsage: canonical.NewUnknownTokenUsage()}
	frame := streamFrame{Type: "response.output_item.added"}
	frame.Item.Type = "mcp_call"
	frame.Item.ID = "mcp_1"
	frame.Item.Name = "Read"
	if handled, _, err := s.handleFrame(context.Background(), frame); err == nil || handled {
		t.Fatalf("MCP frame handled=%v err=%v", handled, err)
	}
}
