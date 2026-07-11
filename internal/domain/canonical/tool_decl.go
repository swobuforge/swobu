package canonical

import (
	"encoding/json"
	"strings"
)

// ToolSchema stores semantic tool-input schema as one JSON object payload.
type ToolSchema struct {
	rawObject string
}

func EmptyToolSchema() ToolSchema {
	return ToolSchema{}
}

func NewToolSchemaObject(raw string) ToolSchema {
	return ToolSchema{rawObject: strings.TrimSpace(raw)} // swobu:io-string source=domain
}

func (s ToolSchema) RawObject() string {
	return s.rawObject
}

func (s ToolSchema) IsEmpty() bool {
	return strings.TrimSpace(s.rawObject) == "" // swobu:io-string source=domain
}

func cloneToolDecls(tools []ToolDecl) []ToolDecl {
	if tools == nil {
		return nil
	}
	cloned := make([]ToolDecl, len(tools))
	for i := range tools {
		if tools[i] == nil {
			continue
		}
		cloned[i] = tools[i].Clone()
	}
	return cloned
}

type requestToolDeclMetadataDTO struct {
	Kind             string          `json:"kind,omitempty"`
	ID               string          `json:"id,omitempty"`
	Name             string          `json:"name,omitempty"`
	Description      string          `json:"description,omitempty"`
	InputSchema      json.RawMessage `json:"input_schema,omitempty"`
	Capability       string          `json:"capability,omitempty"`
	CapabilityConfig json.RawMessage `json:"capability_config,omitempty"`
	Execution        string          `json:"execution,omitempty"`
}

type requestToolPolicyMetadataDTO struct {
	Mode     string `json:"mode"`
	Specific string `json:"specific,omitempty"`
}

func encodeRequestToolDeclsMetadata(tools []ToolDecl) (string, error) {
	if len(tools) == 0 {
		return "", nil
	}
	dto := make([]requestToolDeclMetadataDTO, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			return "", BadRequest("canonical request tool declarations are invalid")
		}
		execution := strings.TrimSpace(string(tool.Owner())) // swobu:io-string source=domain
		switch decl := tool.(type) {
		case FunctionToolDecl:
			schema, err := encodeToolSchemaMetadata(decl.ToolInputSchema())
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(decl.ToolName()) == "" { // swobu:io-string source=domain
				return "", BadRequest("canonical request tool declarations require a name")
			}
			dto = append(dto, requestToolDeclMetadataDTO{
				Kind:        "function",
				ID:          strings.TrimSpace(string(decl.ToolID())),
				Name:        strings.TrimSpace(decl.ToolName()),
				Description: strings.TrimSpace(decl.ToolDescription()),
				InputSchema: schema,
				Execution:   execution,
			})
		case *FunctionToolDecl:
			schema, err := encodeToolSchemaMetadata(decl.ToolInputSchema())
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(decl.ToolName()) == "" { // swobu:io-string source=domain
				return "", BadRequest("canonical request tool declarations require a name")
			}
			dto = append(dto, requestToolDeclMetadataDTO{
				Kind:        "function",
				ID:          strings.TrimSpace(string(decl.ToolID())),
				Name:        strings.TrimSpace(decl.ToolName()),
				Description: strings.TrimSpace(decl.ToolDescription()),
				InputSchema: schema,
				Execution:   execution,
			})
		case CapabilityToolDecl:
			config, err := encodeToolCapabilityConfigMetadata(decl.CapabilityConfig())
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(string(decl.ToolCapability())) == "" { // swobu:io-string source=domain
				return "", BadRequest("canonical request tool declarations require a capability")
			}
			dto = append(dto, requestToolDeclMetadataDTO{
				Kind:             "capability",
				ID:               strings.TrimSpace(string(decl.ToolID())),
				Capability:       strings.TrimSpace(string(decl.ToolCapability())),
				CapabilityConfig: config,
				Execution:        execution,
			})
		case *CapabilityToolDecl:
			config, err := encodeToolCapabilityConfigMetadata(decl.CapabilityConfig())
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(string(decl.ToolCapability())) == "" { // swobu:io-string source=domain
				return "", BadRequest("canonical request tool declarations require a capability")
			}
			dto = append(dto, requestToolDeclMetadataDTO{
				Kind:             "capability",
				ID:               strings.TrimSpace(string(decl.ToolID())),
				Capability:       strings.TrimSpace(string(decl.ToolCapability())),
				CapabilityConfig: config,
				Execution:        execution,
			})
		default:
			return "", InternalError("canonical request tool declarations contain an unsupported tool declaration type")
		}
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		return "", InternalError("canonical request tool declarations could not be encoded")
	}
	return string(raw), nil
}

func decodeRequestToolDeclsMetadata(raw string) ([]ToolDecl, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var dto []requestToolDeclMetadataDTO
	if err := json.Unmarshal([]byte(trimmed), &dto); err != nil {
		return nil, BadRequest("canonical request tool declarations are invalid")
	}
	tools := make([]ToolDecl, 0, len(dto))
	for _, tool := range dto {
		execution := normalizeToolExecutionOwner(ToolExecutionOwner(strings.TrimSpace(tool.Execution)))
		kind := strings.ToLower(strings.TrimSpace(tool.Kind))
		if kind == "" {
			if strings.TrimSpace(tool.Capability) != "" {
				kind = "capability"
			} else {
				kind = "function"
			}
		}
		switch kind {
		case "function":
			schema, err := decodeToolSchemaMetadata(tool.InputSchema)
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(tool.Name) // swobu:io-string source=domain
			if name == "" {
				return nil, BadRequest("canonical request tool declarations require a name")
			}
			decl := NewFunctionToolDecl(tool.ID, name, tool.Description, schema)
			decl.Execution = execution
			tools = append(tools, decl)
		case "capability":
			if strings.TrimSpace(tool.Capability) == "" {
				return nil, BadRequest("canonical request tool declarations require a capability")
			}
			config, err := decodeToolCapabilityConfigMetadata(tool.CapabilityConfig)
			if err != nil {
				return nil, err
			}
			decl := NewCapabilityToolDecl(tool.ID, NewToolCapability(tool.Capability), config)
			decl.Execution = execution
			tools = append(tools, decl)
		default:
			return nil, BadRequest("canonical request tool declarations contain an unsupported kind")
		}
	}
	return tools, nil
}

func encodeToolSchemaMetadata(schema ToolSchema) (json.RawMessage, error) {
	raw := strings.TrimSpace(schema.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, BadRequest("canonical request tool declarations require input_schema")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, BadRequest("canonical request tool declarations require a JSON object input_schema")
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return nil, InternalError("canonical request tool declarations could not be encoded")
	}
	return json.RawMessage(normalized), nil
}

func decodeToolSchemaMetadata(raw json.RawMessage) (ToolSchema, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return ToolSchema{}, BadRequest("canonical request tool declarations require input_schema")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ToolSchema{}, BadRequest("canonical request tool declarations are invalid")
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return ToolSchema{}, InternalError("canonical request tool declarations could not be decoded")
	}
	return NewToolSchemaObject(string(normalized)), nil
}

func encodeToolCapabilityConfigMetadata(config ToolCapabilityConfig) (json.RawMessage, error) {
	raw := strings.TrimSpace(config.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, BadRequest("canonical request tool capability config must be a JSON object")
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return nil, InternalError("canonical request tool capability config could not be encoded")
	}
	return json.RawMessage(normalized), nil
}

func decodeToolCapabilityConfigMetadata(raw json.RawMessage) (ToolCapabilityConfig, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return EmptyToolCapabilityConfig(), nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ToolCapabilityConfig{}, BadRequest("canonical request tool capability config is invalid")
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return ToolCapabilityConfig{}, InternalError("canonical request tool capability config could not be decoded")
	}
	return NewToolCapabilityConfigObject(string(normalized)), nil
}

func encodeToolPolicyMetadata(policy ToolPolicy) (string, error) {
	if err := policy.Validate(); err != nil {
		return "", err
	}
	dto := requestToolPolicyMetadataDTO{
		Mode: string(policy.Mode),
	}
	if specific, ok := policy.SpecificID(); ok {
		dto.Specific = specific.String()
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		return "", InternalError("canonical request tool policy could not be encoded")
	}
	return string(raw), nil
}

func decodeToolPolicyMetadata(raw string) (ToolPolicy, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return NewToolPolicy(ToolPolicyNone, nil), nil
	}
	var dto requestToolPolicyMetadataDTO
	if err := json.Unmarshal([]byte(trimmed), &dto); err != nil {
		return ToolPolicy{}, BadRequest("canonical request tool policy is invalid")
	}
	mode, ok := ParseToolPolicyMode(dto.Mode)
	if !ok {
		return ToolPolicy{}, BadRequest("canonical request tool policy mode is invalid")
	}
	if strings.TrimSpace(dto.Specific) != "" {
		if mode != ToolPolicySpecific {
			return ToolPolicy{}, BadRequest("canonical request tool policy specific mode is invalid")
		}
		specific := NewSemanticToolID(dto.Specific)
		return NewToolPolicy(ToolPolicySpecific, &specific), nil
	}
	if mode == ToolPolicySpecific {
		return ToolPolicy{}, BadRequest("canonical request tool policy specific mode requires a tool id")
	}
	return NewToolPolicy(mode, nil), nil
}
