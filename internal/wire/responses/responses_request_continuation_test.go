package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type realizedResponsesBody struct {
	PreviousResponseID string `json:"previous_response_id"`
	Input              any    `json:"input"`
}

func TestEncode_OmitsInputForContinuationOnlyRequests(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "claude-opus-4-6",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "prior output")},
		Turn:  canonical.NewTurnRef("resp_123"),
	})

	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("Encode returned err=%v", err)
	}
	raw := wire.Raw
	var body realizedResponsesBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if body.PreviousResponseID != "resp_123" {
		t.Fatalf("previous_response_id=%q want resp_123; raw=%s", body.PreviousResponseID, string(raw))
	}
	if body.Input != nil {
		t.Fatalf("input=%#v want nil for continuation-only request; raw=%s", body.Input, string(raw))
	}
}

func TestEncode_KeepsLastTurnInputWithPreviousResponseID(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "claude-opus-4-6",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorAssistant, "prior output"),
			canonical.NewTextItem(canonical.ItemAuthorUser, "new user turn"),
		},
		Turn: canonical.NewTurnRef("resp_123"),
	})

	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("Encode returned err=%v", err)
	}
	raw := wire.Raw
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, ok := body["input"].(string); !ok || got != "new user turn" {
		t.Fatalf("input=%#v want current user turn string; raw=%s", body["input"], string(raw))
	}
}

func TestEncode_PreservesFullThreadWithoutPreviousResponseID(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "claude-opus-4-6",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "open the file"),
			canonical.NewToolUseItem(canonical.ItemAuthorAssistant, "msg_1", "call_1", "Read", canonical.NewToolArgumentsObject(`{"path":"workspace/file.txt"}`)),
			canonical.NewToolResultItem(canonical.ItemAuthorTool, "call_1", "file contents"),
			canonical.NewTextItem(canonical.ItemAuthorUser, "new user turn"),
		},
	})

	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("Encode returned err=%v", err)
	}
	raw := wire.Raw
	var body realizedResponsesBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	input, ok := body.Input.([]any)
	if !ok {
		t.Fatalf("input=%T want array; raw=%s", body.Input, string(raw))
	}
	if len(input) != 4 {
		t.Fatalf("input len = %d, want 4; raw=%s", len(input), string(raw))
	}
	types := []string{"message", "function_call", "function_call_output", "message"}
	for i, item := range input {
		typed, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("input[%d] = %T, want object; raw=%s", i, item, string(raw))
		}
		if got := typed["type"]; got != types[i] {
			t.Fatalf("input[%d].type = %v, want %q; raw=%s", i, got, types[i], string(raw))
		}
	}
}
