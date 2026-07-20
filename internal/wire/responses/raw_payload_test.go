package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestEncode_PreservesCustomToolFormat(t *testing.T) {
	formatRaw := `{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`
	customTool := canonicaltest.MustCustomTool(canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "apply_patch"), "edit files", canonical.NewToolFormatObject(canonicaltest.Object(t, formatRaw)))
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-4o-mini"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		Tools: canonicaltest.SpecifiedToolSet(t, customTool),
	})
	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatal(err)
	}
	tools := body["tools"].([]any)
	custom := tools[0].(map[string]any)
	format := custom["format"].(map[string]any)
	if custom["name"] != "apply_patch" || format["syntax"] != "lark" {
		t.Fatalf("custom tool = %#v", custom)
	}
}

func TestEncode_PreservesInstructions(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        canonical.Specify("gpt-4o-mini"),
		Instructions: canonical.Specify(canonical.NewSystemInstructionSet("Use native tools for filesystem work.")),
		Items:        []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "inspect files")},
	})
	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["instructions"] != "Use native tools for filesystem work." || body["input"] != "inspect files" {
		t.Fatalf("body = %#v", body)
	}
}
