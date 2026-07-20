package responses

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestResponsesEventReader_AcceptsCustomToolCallStreamFrames(t *testing.T) {
	t.Parallel()

	s := &responsesEventReader{
		exchangeID:    "ex",
		responseEnvID: "ex:response:0",
		toolStates:    map[string]responsesToolState{},
		toolInputs:    map[string]string{},
		latestUsage:   canonical.NewUnknownTokenUsage(),
		request:       canonical.NewCanonicalRequest(canonical.RequestParams{Tools: canonicaltest.SpecifiedToolSet(t, canonicaltest.MustCustomTool(canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "apply_patch"), "", canonical.NewToolFormatObject(canonicaltest.Object(t, `{"type":"grammar"}`))))}),
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
			ID:     "custom_1",
			Type:   "custom_tool_call",
			CallID: "call_1",
			Name:   "apply_patch",
		},
	})
	if err != nil {
		t.Fatalf("handleFrame(output_item.added) returned error: %v", err)
	}
	if !handled {
		t.Fatal("handleFrame(output_item.added) not handled")
	}
	if state := s.toolStates["custom_1"]; state.toolType != canonical.ToolTypeCustom {
		t.Fatalf("tool type = %q, want %q", state.toolType, canonical.ToolTypeCustom)
	}

	handled, _, err = s.handleFrame(context.Background(), streamFrame{
		Type:      "response.custom_tool_call_input.delta",
		ItemID:    "custom_1",
		CallID:    "call_1",
		Name:      "apply_patch",
		Delta:     "patch contents",
		Input:     "",
		Arguments: "",
	})
	if err != nil {
		t.Fatalf("handleFrame(arguments.delta) returned error: %v", err)
	}
	if !handled {
		t.Fatal("handleFrame(arguments.delta) not handled")
	}
	if got := s.toolInputs["custom_1"]; got != "patch contents" {
		t.Fatalf("tool input = %q, want patch contents", got)
	}

	handled, _, err = s.handleFrame(context.Background(), streamFrame{
		Type:      "response.custom_tool_call_input.done",
		ItemID:    "custom_1",
		CallID:    "call_1",
		Name:      "apply_patch",
		Input:     "patch contents",
		Arguments: "",
		Item: struct {
			ID          string `json:"id"`
			Type        string `json:"type"`
			CallID      string `json:"call_id"`
			Name        string `json:"name"`
			Arguments   string `json:"arguments"`
			Input       string `json:"input"`
			ServerLabel string `json:"server_label"`
		}{
			ID:     "custom_1",
			Type:   "custom_tool_call",
			CallID: "call_1",
			Name:   "apply_patch",
			Input:  "patch contents",
		},
	})
	if err != nil {
		t.Fatalf("handleFrame(arguments.done) returned error: %v", err)
	}
	if !handled {
		t.Fatal("handleFrame(arguments.done) not handled")
	}
	if _, ok := s.toolStates["custom_1"]; !ok {
		t.Fatal("tool state closed before output_item.done checkpoint")
	}
	frame := streamFrame{Type: "response.output_item.done"}
	frame.Item.ID = "custom_1"
	frame.Item.Type = "custom_tool_call"
	frame.Item.CallID = "call_1"
	frame.Item.Name = "apply_patch"
	frame.Item.Input = "patch contents"
	if handled, _, err := s.handleFrame(context.Background(), frame); err != nil || !handled {
		t.Fatalf("output_item.done handled=%v err=%v", handled, err)
	}
	if _, ok := s.toolStates["custom_1"]; ok {
		t.Fatal("tool state remained after completed checkpoint")
	}
	if _, ok := s.toolInputs["custom_1"]; ok {
		t.Fatal("tool inputs remained after completed checkpoint")
	}
}
