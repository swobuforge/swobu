package responses

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type realizedResponsesBody struct {
	PreviousResponseID string `json:"previous_response_id"`
	Input              any    `json:"input"`
}

func TestEncode_OmitsInputForContinuationOnlyRequests(t *testing.T) {
	req := canonical.NewGenerationRequest(canonical.GenerationRequestParams{
		Model:              "claude-opus-4-6",
		Thread:             []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "prior output")},
		LastTurn:           nil,
		PreviousResponseID: "resp_123",
	})

	wire, err := Encode(req, false)
	if err != nil {
		t.Fatalf("Encode returned err=%v", err)
	}
	raw, err := io.ReadAll(wire.Body)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}
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
	req := canonical.NewGenerationRequest(canonical.GenerationRequestParams{
		Model: "claude-opus-4-6",
		Thread: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorAssistant, "prior output"),
			canonical.NewTextItem(canonical.ItemAuthorUser, "new user turn"),
		},
		LastTurn: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "new user turn"),
		},
		PreviousResponseID: "resp_123",
	})

	wire, err := Encode(req, false)
	if err != nil {
		t.Fatalf("Encode returned err=%v", err)
	}
	raw, err := io.ReadAll(wire.Body)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := body["input"]; !ok {
		t.Fatalf("input missing with non-empty last turn; raw=%s", string(raw))
	}
}
