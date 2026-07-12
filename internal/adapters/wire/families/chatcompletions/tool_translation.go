package chatcompletions

import (
	"encoding/json"
	"fmt"
	"strings"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// swobu:lint ignore string-switch because=protocol boundary decodes tool declaration variants.
func decodeChatCompletionsTools(tools []chatCompletionsToolDefinitionDTO) ([]canonical.ToolDecl, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]canonical.ToolDecl, 0, len(tools))
	for _, tool := range tools {
		kind := strings.ToLower(strings.TrimSpace(tool.Type))
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
			id, leaf, err := canonical.ParseProjectedToolName(name, canonical.ToolKindFunction)
			if err != nil {
				return nil, err
			}
			decl := canonical.NewFunctionToolDecl(id.String(), leaf, tool.Function.Description, schema)
			decl.Strict = cloneBoolPointer(tool.Function.Strict)
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
			id, leaf, err := canonical.ParseProjectedToolName(name, canonical.ToolKindCustom)
			if err != nil {
				return nil, err
			}
			decl := canonical.NewCustomToolDecl(id.String(), leaf, tool.Custom.Description, format)
			out = append(out, decl)
		default:
			return nil, canonical.BadRequest("chat completions request contains an unsupported tool type")
		}
	}
	return out, nil
}

func chatCompletionsToolParametersFromWire(raw json.RawMessage) (canonical.ToolSchema, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return canonical.ToolSchema{}, canonical.BadRequest("chat completions request tool declarations require parameters")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(trimmed), "chat completions request tool declaration parameters are invalid"); err != nil {
		return canonical.ToolSchema{}, err
	}
	return canonical.NewToolSchemaObject(trimmed), nil
}

func encodeChatCompletionsTools(tools []canonical.ToolDecl) ([]chatCompletionsToolDefinitionDTO, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]chatCompletionsToolDefinitionDTO, 0, len(tools))
	for _, tool := range tools {
		wire, err := encodeChatCompletionsTool(tool)
		if err != nil {
			return nil, err
		}
		out = append(out, wire)
	}
	return out, nil
}

func encodeChatCompletionsTool(tool canonical.ToolDecl) (chatCompletionsToolDefinitionDTO, error) {
	if tool == nil {
		return chatCompletionsToolDefinitionDTO{}, canonical.BadRequest("chat completions request tool declarations are invalid")
	}
	switch decl := tool.(type) {
	case canonical.FunctionToolDecl:
		return encodeChatCompletionsFunctionToolDecl(decl)
	case *canonical.FunctionToolDecl:
		return encodeChatCompletionsFunctionToolDecl(*decl)
	case canonical.CustomToolDecl:
		return encodeChatCompletionsCustomToolDecl(decl)
	case *canonical.CustomToolDecl:
		return encodeChatCompletionsCustomToolDecl(*decl)
	default:
		return chatCompletionsToolDefinitionDTO{}, canonical.UnsupportedOperation("chat completions protocol only supports function and custom tool declarations; got " + chatCompletionsUnsupportedToolKind(tool))
	}
}

func encodeChatCompletionsFunctionToolDecl(decl canonical.FunctionToolDecl) (chatCompletionsToolDefinitionDTO, error) {
	name, err := canonical.ProjectedToolName(decl)
	if err != nil {
		return chatCompletionsToolDefinitionDTO{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return chatCompletionsToolDefinitionDTO{}, canonical.BadRequest("chat completions request tool declarations require a name")
	}
	parameters, err := chatCompletionsToolParametersFromSchema(decl.ToolInputSchema())
	if err != nil {
		return chatCompletionsToolDefinitionDTO{}, err
	}
	wire := chatCompletionsToolDefinitionDTO{
		Type: "function",
		Function: &chatCompletionsToolDefinitionFunctionDTO{
			Name:        name,
			Description: strings.TrimSpace(decl.ToolDescription()), // swobu:io-string source=boundary
			Parameters:  parameters,
		},
	}
	if decl.Strict != nil {
		wire.Function.Strict = cloneBoolPointer(decl.Strict)
	}
	return wire, nil
}

func encodeChatCompletionsCustomToolDecl(decl canonical.CustomToolDecl) (chatCompletionsToolDefinitionDTO, error) {
	name, err := canonical.ProjectedToolName(decl)
	if err != nil {
		return chatCompletionsToolDefinitionDTO{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return chatCompletionsToolDefinitionDTO{}, canonical.BadRequest("chat completions request custom tools require a name")
	}
	wire := chatCompletionsToolDefinitionDTO{
		Type: "custom",
		Custom: &chatCompletionsToolDefinitionCustomDTO{
			Name:        name,
			Description: strings.TrimSpace(decl.ToolDescription()), // swobu:io-string source=boundary
		},
	}
	if !decl.Format.IsEmpty() {
		format, err := chatCompletionsToolFormatFromCanonical(decl.Format)
		if err != nil {
			return chatCompletionsToolDefinitionDTO{}, err
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
	rawText := string(raw) // swobu:io-string source=domain
	if strings.TrimSpace(rawText) == "" || strings.TrimSpace(rawText) == "null" {
		return canonical.EmptyToolFormat(), nil
	}
	if _, err := sse.DecodeJSONObject(raw, "chat completions request custom tool format is invalid"); err != nil {
		return canonical.ToolFormat{}, err
	}
	return canonical.NewToolFormatObject(rawText), nil
}

func chatCompletionsUnsupportedToolKind(tool canonical.ToolDecl) string {
	kind := strings.TrimSpace(canonical.ToolDeclKind(tool))
	if kind != "" {
		return kind
	}
	typeName := strings.TrimPrefix(strings.TrimSpace(fmt.Sprintf("%T", tool)), "*")
	if typeName != "" {
		return typeName
	}
	return "unknown"
}

func decodeChatCompletionsToolChoice(raw json.RawMessage, tools []canonical.ToolDecl) (canonical.ToolPolicy, error) {
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
		resolved, resolvedType, err := canonical.ResolveToolDeclByName(tools, name, canonical.ToolTypeFunction)
		if err != nil {
			return canonical.ToolPolicy{}, err
		}
		specificID := resolved.ToolID()
		policy := canonical.NewToolPolicy(canonical.ToolPolicySpecific, &specificID)
		policy.SpecificType = resolvedType
		return policy, nil
	case "custom":
		name := strings.TrimSpace(objectMode.Custom.Name) // swobu:io-string source=provider-wire
		if name == "" {
			return canonical.ToolPolicy{}, canonical.BadRequest("chat completions request tool_choice specific requires a tool name")
		}
		resolved, resolvedType, err := canonical.ResolveToolDeclByName(tools, name, canonical.ToolTypeCustom)
		if err != nil {
			return canonical.ToolPolicy{}, err
		}
		specificID := resolved.ToolID()
		policy := canonical.NewToolPolicy(canonical.ToolPolicySpecific, &specificID)
		policy.SpecificType = resolvedType
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

func hasChatCompletionsCustomTools(tools []canonical.ToolDecl) bool {
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		if canonical.ToolDeclKind(tool) == canonical.ToolTypeCustom {
			return true
		}
	}
	return false
}

// swobu:lint ignore string-switch because=protocol boundary encodes specific tool-choice variants.
func encodeChatCompletionsToolChoice(policy canonical.ToolPolicy, tools []canonical.ToolDecl) (any, error) {
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
			return nil, canonical.BadRequest("chat completions request tool_choice required requires at least one tool")
		}
		return "required", nil
	case canonical.ToolPolicySpecific:
		specific, ok := policy.SpecificID()
		if !ok {
			return nil, canonical.BadRequest("chat completions request tool_choice specific requires a tool id")
		}
		specificType, _ := policy.SpecificToolType()
		decl, resolvedType, err := canonical.ResolveToolDeclByID(tools, specific, specificType)
		if err != nil {
			return nil, err
		}
		name, err := canonical.ProjectedToolName(decl)
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
		default:
			return nil, canonical.UnsupportedOperation("chat completions protocol only supports function and custom specific tool choice")
		}
	default:
		return nil, canonical.BadRequest("chat completions request tool_choice is invalid")
	}
}
