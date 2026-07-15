package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestEncode_PreservesRawCustomToolFormatAndToolSearchParameters(t *testing.T) {
	formatRaw := `{"type":"grammar", "syntax":"lark", "definition":"start: begin_patch hunk+ end_patch"}`
	toolSearchParametersRaw := `{"type":"object", "properties":{"query":{"type":"string"}}}`
	customTool := canonical.CustomToolDecl{
		ID:          canonical.NewSemanticToolID("apply_patch"),
		Name:        "apply_patch",
		Description: "edit files",
		Format:      canonical.NewToolFormatObject(formatRaw),
		Execution:   canonical.ToolOwnerClient,
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-4o-mini",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Tools: []canonical.ToolDecl{
			customTool,
			canonical.CapabilityToolDecl{
				ID:         canonical.NewSemanticToolID("tool_search"),
				Capability: canonical.NewToolCapability("tool_search"),
				Config:     canonical.NewToolCapabilityConfigObject(`{"parameters":` + toolSearchParametersRaw + `}`),
				Execution:  canonical.ToolOwnerClient,
			},
		},
	})

	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	tools, ok := body["tools"].([]any)
	if !ok {
		t.Fatalf("tools = %T, want []any", body["tools"])
	}
	if len(tools) != 2 {
		t.Fatalf("tools len = %d, want 2", len(tools))
	}
	custom, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tools[0] = %T, want map[string]any", tools[0])
	}
	format, ok := custom["format"].(map[string]any)
	if !ok {
		t.Fatalf("custom format = %T, want map[string]any", custom["format"])
	}
	if format["type"] != "grammar" || format["syntax"] != "lark" {
		t.Fatalf("custom format = %#v, want grammar/lark", format)
	}
	if format["definition"] != "start: begin_patch hunk+ end_patch" {
		t.Fatalf("custom format definition = %v, want tool definition", format["definition"])
	}
	if custom["name"] != "apply_patch" {
		t.Fatalf("custom name = %v, want %q", custom["name"], "apply_patch")
	}
	capability, ok := tools[1].(map[string]any)
	if !ok {
		t.Fatalf("tools[1] = %T, want map[string]any", tools[1])
	}
	if capability["type"] != "tool_search" {
		t.Fatalf("tool_search type = %v, want tool_search", capability["type"])
	}
	parameters, ok := capability["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("tool_search parameters = %T, want map[string]any", capability["parameters"])
	}
	if parameters["type"] != "object" {
		t.Fatalf("tool_search parameters.type = %v, want object", parameters["type"])
	}
	props, ok := parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("tool_search parameters.properties = %T, want map[string]any", parameters["properties"])
	}
	query, ok := props["query"].(map[string]any)
	if !ok {
		t.Fatalf("tool_search query = %T, want map[string]any", props["query"])
	}
	if query["type"] != "string" {
		t.Fatalf("tool_search query.type = %v, want string", query["type"])
	}
}

func TestEncode_PreservesInstructions(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "gpt-4o-mini",
		Instructions: "Use native tools for filesystem work.",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "inspect files")},
	})

	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := body["instructions"]; got != "Use native tools for filesystem work." {
		t.Fatalf("instructions = %#v, want canonical instructions", got)
	}
	if got := body["input"]; got != "inspect files" {
		t.Fatalf("input = %#v, want user item only", got)
	}
}
