package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func encodeResponsesTools(tools []canonical.ToolDeclaration, sink compat.Sink, exchangeID string) ([]any, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		wire, err := encodeResponsesTool(tool)
		if err != nil {
			return nil, err
		}
		out = append(out, wire)
	}
	return out, nil
}

func encodeResponsesTool(tool canonical.ToolDeclaration) (map[string]any, error) {
	if tool.Kind() == "" {
		return nil, canonical.BadRequest("response request tool declarations are invalid")
	}
	if decl, ok := tool.Function(); ok {
		return encodeResponsesFunctionTool(tool, decl)
	}
	if decl, ok := tool.Custom(); ok {
		return encodeResponsesCustomTool(tool, decl)
	}
	return nil, canonical.UnsupportedOperation("responses protocol only supports known tool declaration types")
}

func encodeResponsesFunctionTool(declaration canonical.ToolDeclaration, decl canonical.FunctionTool) (map[string]any, error) {
	name, err := responsesToolWireName(declaration)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("response request tool declarations require a name")
	}
	parameters, err := responsesToolParametersFromSchema(decl.InputSchema())
	if err != nil {
		return nil, err
	}
	wire := map[string]any{
		"type":        "function",
		"name":        name,
		"description": strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		"parameters":  parameters,
	}
	if strict, ok := decl.Strict().Get(); ok {
		wire["strict"] = strict
	}
	return wire, nil
}

func encodeResponsesCustomTool(declaration canonical.ToolDeclaration, decl canonical.CustomTool) (map[string]any, error) {
	name, err := responsesToolWireName(declaration)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("response request custom tool declarations require a name")
	}
	format, err := responsesToolFormatFromCanonical(decl.Format())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type":        "custom",
		"name":        name,
		"description": strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		"format":      format,
	}, nil
}

func responsesToolParametersFromSchema(schema canonical.ToolSchema) (any, error) {
	raw := strings.TrimSpace(schema.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, canonical.BadRequest("response request tool declarations require input_schema")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(raw), "response request tool declaration input_schema is invalid"); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func responsesToolFormatFromCanonical(format canonical.ToolFormat) (any, error) {
	raw := strings.TrimSpace(format.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, canonical.BadRequest("response request tool declarations require format")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(raw), "response request tool declaration format is invalid"); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func encodeToolChoice(policy canonical.ToolPolicy, tools []canonical.ToolDeclaration, sink compat.Sink, exchangeID string) (any, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	switch policy.Mode {
	case canonical.ToolPolicyNone:
		return "none", nil
	case canonical.ToolPolicyAuto:
		return "auto", nil
	case canonical.ToolPolicyRequired:
		if len(tools) == 0 {
			return nil, canonical.BadRequest("response request tool_choice required requires at least one tool")
		}
		return "required", nil
	case canonical.ToolPolicySpecific:
		specific, ok := policy.SpecificID()
		if !ok {
			return nil, canonical.BadRequest("response request tool_choice specific requires a tool id")
		}
		specificType := string(specific.Kind())
		decl, resolvedType, err := canonical.ResolveToolDeclarationByKey(tools, specific, specificType)
		if err != nil {
			return nil, err
		}
		name, err := responsesToolWireName(decl)
		if err != nil {
			return nil, err
		}
		if resolvedType == "" {
			resolvedType = "function"
		}
		return map[string]any{
			"type": resolvedType,
			"name": name,
		}, nil
	default:
		return nil, canonical.BadRequest("response request tool_choice is invalid")
	}
}

type responsesToolNamespaceContext struct {
	path         []string
	descriptions []string
	index        int
}

func cloneBoolPointer(ptr *bool) *bool {
	if ptr == nil {
		return nil
	}
	cloned := *ptr
	return &cloned
}
