package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
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

	wire, err := EncodeCarrierWithDecisions(EncodeInput{
		Request:            req,
		NativeContinuation: &provider.NativeContinuation{ID: "resp_123"},
	}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
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

	wire, err := EncodeCarrierWithDecisions(EncodeInput{
		Request:            req,
		NativeContinuation: &provider.NativeContinuation{ID: "resp_123"},
	}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
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

func TestEncode_EmitsExplicitClearsWithNativeContinuation(t *testing.T) {
	presence := canonical.RequestPresence{
		Instructions: true,
		Tools:        true,
		OutputFormat: true,
		Controls: canonical.GenerationControlsPresence{
			MaxOutputTokens: true,
			StopSequences:   true,
			Temperature:     true,
			TopP:            true,
		},
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    "gpt-4o",
		Items:    []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "continue")},
		Tools:    []canonical.ToolDecl{},
		Controls: canonical.GenerationControls{Limits: canonical.GenerationLimits{StopSequences: []string{}}},
		Presence: presence,
	})

	wire, err := EncodeCarrierWithDecisions(EncodeInput{
		Request:            req,
		NativeContinuation: &provider.NativeContinuation{ID: "resp_123"},
	}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatal(err)
	}
	if value, ok := body["instructions"]; !ok || value != "" {
		t.Fatalf("instructions = %#v, present=%v", value, ok)
	}
	if value, ok := body["tools"].([]any); !ok || len(value) != 0 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	for _, field := range []string{"max_output_tokens", "temperature", "top_p"} {
		if value, ok := body[field]; !ok || value != nil {
			t.Fatalf("%s = %#v, present=%v", field, value, ok)
		}
	}
	if value, ok := body["stop"].([]any); !ok || len(value) != 0 {
		t.Fatalf("stop = %#v", body["stop"])
	}
	text, ok := body["text"].(map[string]any)
	if !ok {
		t.Fatalf("text = %#v", body["text"])
	}
	format, ok := text["format"].(map[string]any)
	if !ok || format["type"] != "text" {
		t.Fatalf("text.format = %#v", text["format"])
	}
}

func TestEncode_EmitsDefaultParallelPolicyToClearInheritedRestriction(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-4o",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "continue")},
		Tools: []canonical.ToolDecl{
			canonical.NewFunctionToolDecl("search", "search", "search", canonical.NewToolSchemaObject(`{"type":"object"}`)),
		},
		Presence: canonical.RequestPresence{ToolCallBatch: true},
	})
	wire, err := EncodeCarrierWithDecisions(EncodeInput{
		Request:            req,
		NativeContinuation: &provider.NativeContinuation{ID: "resp_123"},
	}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatal(err)
	}
	if value, ok := body["parallel_tool_calls"].(bool); !ok || !value {
		t.Fatalf("parallel_tool_calls = %#v", body["parallel_tool_calls"])
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
