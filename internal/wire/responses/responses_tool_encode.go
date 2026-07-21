package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

// ProviderRequestTool is one typed Responses tool declaration before exact-
// provider spelling and the single JSON serialization boundary.
type ProviderRequestTool struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      *bool  `json:"strict,omitempty"`
	Format      any    `json:"format,omitempty"`
}

func encodeResponsesTools(tools []canonical.ToolDeclaration, sink compat.Sink, exchangeID string) ([]ProviderRequestTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	for _, tool := range tools {
		if decl, ok := tool.Function(); ok {
			if strict, specified := decl.Strict().Get(); specified && strict {
				if err := emitResponsesRequestDecision(sink, exchangeID, compat.RequestToolsSchemaStrict, compat.Exact); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	out := make([]ProviderRequestTool, 0, len(tools))
	for _, tool := range tools {
		wire, err := encodeResponsesTool(tool)
		if err != nil {
			return nil, err
		}
		out = append(out, wire)
	}
	return out, nil
}

func encodeResponsesTool(tool canonical.ToolDeclaration) (ProviderRequestTool, error) {
	if tool.Kind() == "" {
		return ProviderRequestTool{}, canonical.BadRequest("response request tool declarations are invalid")
	}
	if decl, ok := tool.Function(); ok {
		return encodeResponsesFunctionTool(tool, decl)
	}
	if decl, ok := tool.Custom(); ok {
		return encodeResponsesCustomTool(tool, decl)
	}
	if tool.Kind() == canonical.ToolKindWebSearch {
		return ProviderRequestTool{Type: canonical.ToolTypeWebSearch}, nil
	}
	return ProviderRequestTool{}, canonical.UnsupportedOperation("responses protocol only supports known tool declaration types")
}

func encodeResponsesFunctionTool(declaration canonical.ToolDeclaration, decl canonical.FunctionTool) (ProviderRequestTool, error) {
	name, err := responsesToolWireName(declaration)
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return ProviderRequestTool{}, canonical.BadRequest("response request tool declarations require a name")
	}
	parameters, err := responsesToolParametersFromSchema(decl.InputSchema())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	wire := ProviderRequestTool{
		Type:        "function",
		Name:        name,
		Description: strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		Parameters:  parameters,
	}
	if strict, ok := decl.Strict().Get(); ok {
		wire.Strict = &strict
	}
	return wire, nil
}

func encodeResponsesCustomTool(declaration canonical.ToolDeclaration, decl canonical.CustomTool) (ProviderRequestTool, error) {
	name, err := responsesToolWireName(declaration)
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return ProviderRequestTool{}, canonical.BadRequest("response request custom tool declarations require a name")
	}
	format, err := responsesToolFormatFromCanonical(decl.Format())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	return ProviderRequestTool{
		Type:        "custom",
		Name:        name,
		Description: strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		Format:      format,
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
	if err := policy.ValidateForTools(tools); err != nil {
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
		if resolvedType == canonical.ToolTypeWebSearch {
			return map[string]any{"type": canonical.ToolTypeWebSearch}, nil
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
