package chatcompletions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

// swobu:lint ignore string-switch because=protocol boundary decodes tool declaration variants.
func decodeChatCompletionsTools(tools []ProviderRequestTool, changeLog *[]compat.Change, exchangeID string) ([]canonical.ToolDeclaration, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]canonical.ToolDeclaration, 0, len(tools))
	for index, tool := range tools {
		kind := strings.ToLower(strings.TrimSpace(tool.Type)) // swobu:io-string source=domain
		switch kind {
		case "function":
			if tool.Function == nil {
				return nil, canonical.BadRequest("chat completions request function tools require function")
			}
			schema, err := chatCompletionsToolParametersFromWire(tool.Function.Parameters)
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(tool.Function.Name) // swobu:io-string source=boundary
			if name == "" {
				return nil, canonical.BadRequest("chat completions request tool declarations require a name")
			}
			id, err := canonical.ToolIdentityFromWire(name, canonical.ToolKindFunction)
			if err != nil {
				return nil, err
			}
			strict := canonical.Unspecified[bool]()
			if tool.Function.Strict != nil {
				strict = canonical.Specify(*tool.Function.Strict)
			}
			decl, err := canonical.NewFunctionTool(id, tool.Function.Description, schema, strict)
			if err != nil {
				return nil, err
			}
			out = append(out, decl)
		case "custom":
			if tool.Custom == nil {
				return nil, canonical.BadRequest("chat completions request custom tools require custom")
			}
			format, err := chatCompletionsToolFormatFromWire(tool.Custom.Format)
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(tool.Custom.Name) // swobu:io-string source=boundary
			if name == "" {
				return nil, canonical.BadRequest("chat completions request custom tools require a name")
			}
			id, err := canonical.ToolIdentityFromWire(name, canonical.ToolKindCustom)
			if err != nil {
				return nil, err
			}
			decl, err := canonical.NewCustomTool(id, tool.Custom.Description, format)
			if err != nil {
				return nil, err
			}
			out = append(out, decl)
		default:
			if err := appendChatOccurrenceChange(changeLog, exchangeID, canonical.RequestToolsKind, compat.Omission, canonical.ToolIndexOccurrence(uint32(index))); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func chatCompletionsToolParametersFromWire(raw json.RawMessage) (canonical.ToolSchema, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return canonical.ToolSchema{}, canonical.BadRequest("chat completions request tool declarations require parameters")
	}
	object, err := canonical.ParseJSONObject([]byte(trimmed))
	if err != nil {
		return canonical.ToolSchema{}, err
	}
	return canonical.NewToolSchemaObject(object), nil
}

func encodeChatCompletionsTools(tools []canonical.ToolDeclaration, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) ([]ProviderRequestTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]ProviderRequestTool, 0, len(tools))
	for _, tool := range tools {
		if tool.Kind() == canonical.ToolKindWebSearch {
			continue
		}
		wireTool, err := encodeChatCompletionsTool(tool, names)
		if err != nil {
			return nil, err
		}
		out = append(out, wireTool)
	}
	return out, nil
}

func encodeChatCompletionsTool(tool canonical.ToolDeclaration, names wire.ToolNames) (ProviderRequestTool, error) {
	if tool.Kind() == "" {
		return ProviderRequestTool{}, canonical.BadRequest("chat completions request tool declarations are invalid")
	}
	if decl, ok := tool.Function(); ok {
		return encodeChatCompletionsFunctionTool(tool, decl, names)
	}
	if decl, ok := tool.Custom(); ok {
		return encodeChatCompletionsCustomTool(tool, decl, names)
	}
	return ProviderRequestTool{}, provider.IncompatibleCapability(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key()), "Chat Completions cannot represent canonical tool declaration kind "+chatCompletionsUnsupportedToolKind(tool))
}

func encodeChatCompletionsFunctionTool(declaration canonical.ToolDeclaration, decl canonical.FunctionTool, names wire.ToolNames) (ProviderRequestTool, error) {
	name, err := wire.EncodeToolName(names, declaration.Key())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return ProviderRequestTool{}, canonical.BadRequest("chat completions request tool declarations require a name")
	}
	parameters, err := chatCompletionsToolParametersFromSchema(decl.InputSchema())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	wire := ProviderRequestTool{
		Type: "function",
		Function: &chatCompletionsToolDefinitionFunctionDTO{
			Name:        name,
			Description: strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
			Parameters:  parameters,
		},
	}
	if strict, ok := decl.Strict().Get(); ok {
		wire.Function.Strict = cloneBoolPointer(&strict)
	}
	return wire, nil
}

func encodeChatCompletionsCustomTool(declaration canonical.ToolDeclaration, decl canonical.CustomTool, names wire.ToolNames) (ProviderRequestTool, error) {
	name, err := wire.EncodeToolName(names, declaration.Key())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return ProviderRequestTool{}, canonical.BadRequest("chat completions request custom tools require a name")
	}
	wire := ProviderRequestTool{
		Type: "custom",
		Custom: &chatCompletionsToolDefinitionCustomDTO{
			Name:        name,
			Description: strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		},
	}
	if !decl.Format().IsEmpty() {
		format, err := chatCompletionsToolFormatFromCanonical(decl.Format())
		if err != nil {
			return ProviderRequestTool{}, err
		}
		wire.Custom.Format = format
	}
	return wire, nil
}

func chatCompletionsToolParametersFromSchema(schema canonical.ToolSchema) (json.RawMessage, error) {
	raw := strings.TrimSpace(schema.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, canonical.BadRequest("chat completions request tool declarations require parameters")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(raw), "chat completions request tool declaration parameters are invalid"); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func chatCompletionsToolFormatFromCanonical(format canonical.ToolFormat) (json.RawMessage, error) {
	raw := format.RawObject()
	if strings.TrimSpace(raw) == "" { // swobu:io-string source=domain
		return nil, canonical.BadRequest("chat completions request custom tools require format")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(raw), "chat completions request custom tool format is invalid"); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func chatCompletionsToolFormatFromWire(raw json.RawMessage) (canonical.ToolFormat, error) {
	rawText := string(raw)                                                        // swobu:io-string source=domain
	if strings.TrimSpace(rawText) == "" || strings.TrimSpace(rawText) == "null" { // swobu:io-string source=domain
		return canonical.EmptyToolFormat(), nil
	}
	object, err := canonical.ParseJSONObject(raw)
	if err != nil {
		return canonical.ToolFormat{}, err
	}
	return canonical.NewToolFormatObject(object), nil
}

func chatCompletionsUnsupportedToolKind(tool canonical.ToolDeclaration) string {
	kind := strings.TrimSpace(string(tool.Kind())) // swobu:io-string source=domain
	if kind != "" {
		return kind
	}
	typeName := strings.TrimPrefix(strings.TrimSpace(fmt.Sprintf("%T", tool)), "*") // swobu:io-string source=domain
	if typeName != "" {
		return typeName
	}
	return "unknown"
}

// swobu:lint ignore function-complexity because=chat completions tool-choice decoding keeps all protocol variants in one boundary helper.
func decodeChatCompletionsToolChoice(raw json.RawMessage, tools []canonical.ToolDeclaration, changeLog *[]compat.Change, exchangeID string) (canonical.ToolPolicy, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		if len(tools) > 0 {
			return canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), nil
		}
		return canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), nil
	}

	var stringMode string
	if err := json.Unmarshal([]byte(trimmed), &stringMode); err == nil {
		mode, ok := canonical.ParseToolPolicyMode(stringMode)
		if !ok {
			return canonical.ToolPolicy{}, canonical.BadRequest("chat completions request tool_choice is invalid")
		}
		switch mode {
		case canonical.ToolPolicyNone, canonical.ToolPolicyAuto, canonical.ToolPolicyRequired:
			return canonical.NewToolPolicy(mode, nil), nil
		case canonical.ToolPolicySpecific:
			return canonical.ToolPolicy{}, canonical.BadRequest("chat completions request tool_choice specific requires a tool name")
		}
	}

	var objectMode struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
		Custom struct {
			Name string `json:"name"`
		} `json:"custom"`
	}
	if err := json.Unmarshal([]byte(trimmed), &objectMode); err != nil {
		return canonical.ToolPolicy{}, canonical.BadRequest("chat completions request tool_choice is invalid")
	}
	normalizedType := strings.ToLower(strings.TrimSpace(objectMode.Type)) // swobu:io-string source=provider-wire
	switch normalizedType {
	case "auto":
		return canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), nil
	case "required":
		return canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), nil
	case "function":
		name := strings.TrimSpace(objectMode.Function.Name) // swobu:io-string source=provider-wire
		if name == "" {
			return canonical.ToolPolicy{}, canonical.BadRequest("chat completions request tool_choice specific requires a tool name")
		}
		resolved, _, err := canonical.ResolveToolDeclarationByName(tools, name, canonical.ToolTypeFunction)
		if err != nil {
			return canonical.ToolPolicy{}, err
		}
		specificID := resolved.Key()
		policy := canonical.NewToolPolicy(canonical.ToolPolicySpecific, &specificID)
		return policy, nil
	case "custom":
		name := strings.TrimSpace(objectMode.Custom.Name) // swobu:io-string source=provider-wire
		if name == "" {
			return canonical.ToolPolicy{}, canonical.BadRequest("chat completions request tool_choice specific requires a tool name")
		}
		resolved, _, err := canonical.ResolveToolDeclarationByName(tools, name, canonical.ToolTypeCustom)
		if err != nil {
			return canonical.ToolPolicy{}, err
		}
		specificID := resolved.Key()
		policy := canonical.NewToolPolicy(canonical.ToolPolicySpecific, &specificID)
		return policy, nil
	default:
		return canonical.ToolPolicy{}, canonical.BadRequest("chat completions request tool_choice is invalid")
	}
}

func cloneBoolPointer(ptr *bool) *bool {
	if ptr == nil {
		return nil
	}
	value := *ptr
	return &value
}

// swobu:lint ignore string-switch because=protocol boundary encodes specific tool-choice variants.
func encodeChatCompletionsToolChoice(policy canonical.ToolPolicy, tools []canonical.ToolDeclaration, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) (any, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if len(tools) == 0 {
		switch policy.Mode {
		case canonical.ToolPolicyRequired:
			return nil, canonical.BadRequest("chat completions request tool_choice required requires at least one tool")
		case canonical.ToolPolicySpecific:
			return nil, canonical.BadRequest("chat completions request tool_choice specific requires a tool id")
		default:
			// Empty tool surfaces are inert here. Omit the backend-visible
			// field rather than emitting a no-op choice some backends reject.
			return nil, nil
		}
	}
	switch policy.Mode {
	case canonical.ToolPolicyNone:
		return "none", nil
	case canonical.ToolPolicyAuto:
		return "auto", nil
	case canonical.ToolPolicyRequired:
		return "required", nil
	case canonical.ToolPolicySpecific:
		specific, ok := policy.SpecificID()
		if !ok {
			return nil, canonical.BadRequest("chat completions request tool_choice specific requires a tool id")
		}
		specificType := string(specific.Kind())
		decl, resolvedType, err := canonical.ResolveToolDeclarationByKey(tools, specific, specificType)
		if err != nil {
			return nil, err
		}
		name, err := wire.EncodeToolName(names, decl.Key())
		if err != nil {
			return nil, err
		}
		switch resolvedType {
		case canonical.ToolTypeFunction:
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": name,
				},
			}, nil
		case canonical.ToolTypeCustom:
			return map[string]any{
				"type": "custom",
				"custom": map[string]any{
					"name": name,
				},
			}, nil
		case canonical.ToolTypeWebSearch:
			return map[string]any{
				"type": canonical.ToolTypeWebSearch,
			}, nil
		default:
			return nil, provider.IncompatibleCapability(canonical.RequestToolPolicy, canonical.Occurrence{}, "Chat Completions cannot represent this canonical specific tool choice")
		}
	default:
		return nil, canonical.BadRequest("chat completions request tool_choice is invalid")
	}
}
