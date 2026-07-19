package responses

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestResponsesEventReader_AcceptsMcpCallStreamFrames(t *testing.T) {
	t.Parallel()

	s := &responsesEventReader{
		exchangeID:    "ex",
		responseEnvID: "ex:response:0",
		toolStates:    map[string]responsesToolState{},
		toolInputs:    map[string]string{},
		latestUsage:   canonical.NewUnknownTokenUsage(),
	}

	handled, _, err := s.handleFrame(context.Background(), streamFrame{
		Type: "response.output_item.added",
		Item: struct {
			ID          string `json:"id"`
			Type        string `json:"type"`
			CallID      string `json:"call_id"`
			Name        string `json:"name"`
			Arguments   string `json:"arguments"`
			Input       string `json:"input"`
			ServerLabel string `json:"server_label"`
		}{
			ID:   "mcp_1",
			Type: "mcp_call",
			Name: "Read",
		},
	})
	if err != nil {
		t.Fatalf("handleFrame(output_item.added) returned error: %v", err)
	}
	if !handled {
		t.Fatal("handleFrame(output_item.added) not handled")
	}

	handled, _, err = s.handleFrame(context.Background(), streamFrame{
		Type:      "response.mcp_call_arguments.delta",
		ItemID:    "mcp_1",
		CallID:    "mcp_1",
		Name:      "Read",
		Delta:     `{"path":"workspace/file.txt"}`,
		Arguments: "",
	})
	if err != nil {
		t.Fatalf("handleFrame(arguments.delta) returned error: %v", err)
	}
	if !handled {
		t.Fatal("handleFrame(arguments.delta) not handled")
	}
}
