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
		Model:              "claude-opus-4-6",
		Items:              []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "prior output")},
		PreviousResponseID: "resp_123",
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
		PreviousResponseID: "resp_123",
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
	if _, ok := body["input"]; !ok {
		t.Fatalf("input missing with non-empty last turn; raw=%s", string(raw))
	}
}

func TestEncode_UsesOutputTextPartsForAssistantContent(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "claude-opus-4-6",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorAssistant, "prior output"),
			canonical.NewTextItem(canonical.ItemAuthorUser, "new user turn"),
		},
	})

	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("Encode returned err=%v", err)
	}
	raw := wire.Raw
	var body struct {
		Input []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(body.Input) != 2 {
		t.Fatalf("input len=%d want 2; raw=%s", len(body.Input), string(raw))
	}
	if body.Input[0].Role != "assistant" || len(body.Input[0].Content) != 1 || body.Input[0].Content[0].Type != "output_text" {
		t.Fatalf("assistant content part type mismatch: %#v; raw=%s", body.Input[0], string(raw))
	}
	if body.Input[1].Role != "user" || len(body.Input[1].Content) != 1 || body.Input[1].Content[0].Type != "input_text" {
		t.Fatalf("user content part type mismatch: %#v; raw=%s", body.Input[1], string(raw))
	}
}
