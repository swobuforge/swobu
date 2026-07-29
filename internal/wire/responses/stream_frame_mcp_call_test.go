package responses

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestResponsesEventReaderRejectsKnownProviderMCPStreamStart(t *testing.T) {
	s := &responsesResponseStream{exchangeID: "ex", responseEnvID: "r", providerOutputs: map[int]*pendingResponseOutput{}, latestUsage: canonical.NewUnknownTokenUsage()}
	outputIndex := 0
	frame := streamFrame{Type: "response.output_item.added", OutputIndex: &outputIndex}
	frame.Item.Type = "mcp_call"
	frame.Item.ID = "mcp_1"
	frame.Item.Name = "Read"
	handled, _, err := s.handleFrame(context.Background(), frame)
	var backendError canonical.BackendError
	if handled || !errors.As(err, &backendError) {
		t.Fatalf("MCP frame handled=%v err=%T %v", handled, err, err)
	}
}
