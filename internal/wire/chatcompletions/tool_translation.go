package chatcompletions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
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
	typed, _, _, err := compileChatCompletionsTools(tools, names, changeLog, exchangeID, ToolLowering{})
	return typed, err
}

// DefaultToolLowering returns official Chat Completions semantics.
func DefaultToolLowering() ToolLowering {
	function := func(ctx ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		encoded, err := encodeChatCompletionsTool(tool, ctx.Names)
		if err != nil {
			return ToolProjection{}, nil, err
		}
		return chatFunctionProjection(encoded, func(call canonical.ToolCallItem) (string, error) {
			object, ok := call.Input().Object()
			if !ok {
				return "", canonical.BadRequest("chat completions function projection requires object input")
			}
			return object.String(), nil
		}), nil, nil
	}
	custom := func(ctx ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		encoded, err := encodeChatCompletionsTool(tool, ctx.Names)
		if err != nil {
			return ToolProjection{}, nil, err
		}
		return chatCustomProjection(encoded), nil, nil
	}
	discovery := func(ctx ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		decl, ok := tool.Discovery()
		if !ok || decl.Executor() != canonical.DiscoveryExecutorClient {
			return ToolProjection{}, []compat.Change{compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key()))}, nil
		}
		encoded, err := encodeChatCompletionsDiscoveryTool(tool, decl, ctx.Names)
		if err != nil {
			return ToolProjection{}, nil, err
		}
		return chatFunctionProjection(encoded, func(call canonical.ToolCallItem) (string, error) {
			object, ok := call.Input().Object()
			if !ok {
				return "", canonical.BadRequest("chat completions discovery projection requires object input")
			}
			return object.String(), nil
		}), nil, nil
	}
	omit := func(_ ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		return ToolProjection{}, []compat.Change{compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key()))}, nil
	}
	return ToolLowering{Function: function, Custom: custom, WebSearch: omit, Discovery: discovery}
}

func chatFunctionProjection(encoded ProviderRequestTool, arguments func(canonical.ToolCallItem) (string, error)) ToolProjection {
	return ToolProjection{Fragments: []ToolFragment{{Value: encoded, Standard: &encoded}}, TargetType: "function", TargetName: encoded.Function.Name,
		ProjectCall: func(call canonical.ToolCallItem) (toolCallBody, error) {
			value, err := arguments(call)
			if err != nil {
				return toolCallBody{}, err
			}
			return toolCallBody{ID: call.CallID().String(), Type: "function", Function: &toolFunctionBody{Name: encoded.Function.Name, Arguments: value}}, nil
		},
	}
}

func chatCustomProjection(encoded ProviderRequestTool) ToolProjection {
	return ToolProjection{Fragments: []ToolFragment{{Value: encoded, Standard: &encoded}}, TargetType: "custom", TargetName: encoded.Custom.Name,
		ProjectCall: func(call canonical.ToolCallItem) (toolCallBody, error) {
			text, ok := call.Input().Text()
			if !ok {
				return toolCallBody{}, canonical.BadRequest("chat completions custom calls require text input")
			}
			return toolCallBody{ID: call.CallID().String(), Type: "custom", Custom: &toolCustomBody{Name: encoded.Custom.Name, Input: text}}, nil
		},
	}
}

// FunctionProjection returns a complete Chat Function manifestation using the
// supplied argument projector. Provider construction can therefore replace a
// semantic slot without teaching history serialization the new argument shape.
func FunctionProjection(encoded ProviderRequestTool, arguments func(canonical.ToolCallItem) (string, error)) ToolProjection {
	return chatFunctionProjection(encoded, arguments)
}

// DefaultLowering returns total official Chat Completions semantics.
func DefaultLowering() Lowering {
	return Lowering{
		Tools: DefaultToolLowering(),
		Reasoning: func(req canonical.CanonicalRequest, target ReasoningTargetDialect, changeLog *[]compat.Change, _ string) (map[string]any, error) {
			fields := make(map[string]any)
			if err := encodeChatCompletionsReasoning(fields, req, target, changeLog); err != nil {
				return nil, err
			}
			return fields, nil
		},
		Message: func(*ProviderRequestMessage, []canonical.CanonicalItem) error { return nil },
	}
}

func compileChatCompletionsTools(tools []canonical.ToolDeclaration, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string, lowering ToolLowering) ([]ProviderRequestTool, []any, compiledToolProjection, error) {
	if len(tools) == 0 {
		return nil, nil, compiledToolProjection{occurrences: make(map[canonical.ToolKey]ToolProjection)}, nil
	}
	typed := make([]ProviderRequestTool, 0, len(tools))
	out := make([]any, 0, len(tools))
	projectionSet := compiledToolProjection{lowered: wire.LoweredToolSet{Records: make([]wire.LoweredToolRecord, 0, len(tools))}, occurrences: make(map[canonical.ToolKey]ToolProjection, len(tools))}
	if !lowering.resolved() {
		return nil, nil, compiledToolProjection{}, canonical.InternalError("Chat tool compilation requires resolved lowering")
	}
	for ordinal, tool := range tools {
		var transformer ToolTransformer
		switch tool.Kind() {
		case canonical.ToolKindFunction:
			transformer = lowering.Function
		case canonical.ToolKindCustom:
			transformer = lowering.Custom
		case canonical.ToolKindWebSearch:
			transformer = lowering.WebSearch
		case canonical.ToolKindDiscovery:
			transformer = lowering.Discovery
		}
		if transformer == nil {
			return nil, nil, compiledToolProjection{}, canonical.InternalError("Chat lowering contains an unresolved tool slot")
		}
		projection, changes, err := transformer(ToolLoweringContext{Ordinal: uint32(ordinal), Names: names}, tool)
		if changeLog != nil {
			*changeLog = append(*changeLog, changes...)
		}
		if err != nil {
			return nil, nil, compiledToolProjection{}, err
		}
		for _, fragment := range projection.Fragments {
			out = append(out, fragment.Value)
			if fragment.Standard != nil {
				typed = append(typed, *fragment.Standard)
			}
		}
		projectionSet.lowered.Records = append(projectionSet.lowered.Records, wire.LoweredToolRecord{
			Key: tool.Key(), Kind: tool.Kind(), FragmentCount: len(projection.Fragments), TargetType: projection.TargetType, TargetName: projection.TargetName,
		})
		projectionSet.occurrences[tool.Key()] = projection
	}
	return typed, out, projectionSet, nil
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
	return ProviderRequestTool{}, canonical.InternalError("Chat Completions tool encoder received unsupported declaration kind " + chatCompletionsUnsupportedToolKind(tool))
}

func encodeChatCompletionsFunctionTool(declaration canonical.ToolDeclaration, decl canonical.FunctionTool, names wire.ToolNames) (ProviderRequestTool, error) {
	name, err := wire.EncodeToolName(names, declaration.Key())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return ProviderRequestTool{}, canonical.BadRequest("chat completions request function tools require a name")
	}
	wireTool := ProviderRequestTool{
		Type: "function",
		Function: &chatCompletionsToolDefinitionFunctionDTO{
			Name:        name,
			Description: strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		},
	}
	if strict, specified := decl.Strict().Get(); specified {
		wireTool.Function.Strict = &strict
	}
	if !decl.InputSchema().IsEmpty() {
		parameters, err := chatCompletionsToolParametersFromSchema(decl.InputSchema())
		if err != nil {
			return ProviderRequestTool{}, err
		}
		wireTool.Function.Parameters = parameters
	}
	return wireTool, nil
}

// encodeChatCompletionsDiscoveryTool lowers client-owned discovery through the
// ordinary function grammar; the canonical result still owns tool-environment growth.
func encodeChatCompletionsDiscoveryTool(declaration canonical.ToolDeclaration, discovery canonical.ToolDiscoveryTool, names wire.ToolNames) (ProviderRequestTool, error) {
	name, err := wire.EncodeToolName(names, declaration.Key())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	parameters, err := chatCompletionsToolParametersFromSchema(discovery.InputSchema())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	return ProviderRequestTool{Type: "function", Function: &chatCompletionsToolDefinitionFunctionDTO{
		Name: strings.TrimSpace(name), Description: strings.TrimSpace(discovery.Description()), Parameters: parameters,
	}}, nil
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
func encodeChatCompletionsToolChoice(policy canonical.ToolPolicy, lowered wire.LoweredToolSet, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) (any, error) {
	// Out-of-band projections have a real emitted identity but intentionally no
	// inline declaration fragment. Their owning provider projects policy through
	// its top-level request fields, so Chat must not report policy omission.
	if allChatToolsOutOfBand(lowered) {
		return nil, nil
	}
	if lowered.TotalFragments() == 0 {
		if policy.Mode == canonical.ToolPolicyRequired || policy.Mode == canonical.ToolPolicySpecific {
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{}))
			}
		}
		// Empty surviving surfaces are inert for soft policies. Omit the
		// backend-visible field rather than emitting a no-op choice.
		return nil, nil
	}
	switch policy.Mode {
	case canonical.ToolPolicyNone:
		return "none", nil
	case canonical.ToolPolicyAuto:
		return "auto", nil
	case canonical.ToolPolicyRequired:
		return "required", nil
	case canonical.ToolPolicySpecific:
		key, ok := policy.SpecificID()
		if !ok {
			return nil, canonical.BadRequest("specific tool policy requires a tool id")
		}
		record, ok := lowered.FindSource(key)
		if !ok || record.FragmentCount != 1 {
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{}))
			}
			return nil, nil
		}
		switch record.TargetType {
		case "function":
			name := record.TargetName
			if name == "" {
				var err error
				name, err = wire.EncodeToolName(names, record.Key)
				if err != nil {
					return nil, err
				}
			}
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": strings.TrimSpace(name),
				},
			}, nil
		case "custom":
			name := record.TargetName
			if name == "" {
				var err error
				name, err = wire.EncodeToolName(names, record.Key)
				if err != nil {
					return nil, err
				}
			}
			return map[string]any{
				"type": "custom",
				"custom": map[string]any{
					"name": strings.TrimSpace(name),
				},
			}, nil
		default:
			if record.TargetType == "out_of_band" {
				return nil, nil
			}
			if strings.TrimSpace(record.TargetType) != "" {
				return map[string]any{"type": strings.TrimSpace(record.TargetType)}, nil
			}
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{}))
			}
			return nil, nil
		}
	default:
		return nil, canonical.BadRequest("chat completions request tool_choice is invalid")
	}
}

func allChatToolsOutOfBand(lowered wire.LoweredToolSet) bool {
	if lowered.Len() == 0 {
		return false
	}
	for _, record := range lowered.Records {
		if record.FragmentCount != 0 || record.TargetType != "out_of_band" {
			return false
		}
	}
	return true
}
