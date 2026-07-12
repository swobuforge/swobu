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
	Kind             string                       `json:"kind,omitempty"`
	ID               string                       `json:"id,omitempty"`
	Name             string                       `json:"name,omitempty"`
	Description      string                       `json:"description,omitempty"`
	InputSchema      json.RawMessage              `json:"input_schema,omitempty"`
	Strict           *bool                        `json:"strict,omitempty"`
	Format           json.RawMessage              `json:"format,omitempty"`
	Tools            []requestToolDeclMetadataDTO `json:"tools,omitempty"`
	Capability       string                       `json:"capability,omitempty"`
	CapabilityConfig json.RawMessage              `json:"capability_config,omitempty"`
	Execution        string                       `json:"execution,omitempty"`
}

type requestToolPolicyMetadataDTO struct {
	Mode         string `json:"mode"`
	Specific     string `json:"specific,omitempty"`
	SpecificType string `json:"specific_type,omitempty"`
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
		executionRaw := strings.TrimSpace(tool.Execution) // swobu:io-string source=domain
		execution := normalizeToolExecutionOwner(ToolExecutionOwner(executionRaw))
		kindRaw := strings.TrimSpace(tool.Kind)          // swobu:io-string source=domain
		kind := strings.ToLower(kindRaw)                 // swobu:io-string source=domain
		capability := strings.TrimSpace(tool.Capability) // swobu:io-string source=domain
		if len(tool.Tools) > 0 {
			return nil, BadRequest("canonical request tool namespace declarations are unsupported")
		}
		if kind == "" {
			if capability != "" {
				kind = "capability"
			} else if len(tool.Format) > 0 {
				kind = "custom"
			} else {
				kind = "function"
			}
		}
		if kind == "function" {
			schema, err := decodeToolSchemaMetadata(tool.InputSchema)
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(tool.Name) // swobu:io-string source=domain
			if name == "" {
				return nil, BadRequest("canonical request tool declarations require a name")
			}
			decl := NewFunctionToolDecl(tool.ID, name, tool.Description, schema)
			decl.Strict = cloneBoolPointer(tool.Strict)
			decl.Execution = execution
			tools = append(tools, decl)
		} else if kind == "custom" {
			format, err := decodeToolFormatMetadata(tool.Format)
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(tool.Name) // swobu:io-string source=domain
			if name == "" {
				return nil, BadRequest("canonical request tool declarations require a name")
			}
			decl := NewCustomToolDecl(tool.ID, name, tool.Description, format)
			decl.Execution = execution
			tools = append(tools, decl)
		} else if kind == "capability" {
			if capability == "" {
				return nil, BadRequest("canonical request tool declarations require a capability")
			}
			config, err := decodeToolCapabilityConfigMetadata(tool.CapabilityConfig)
			if err != nil {
				return nil, err
			}
			decl := NewCapabilityToolDecl(tool.ID, NewToolCapability(capability), config)
			decl.Execution = execution
			tools = append(tools, decl)
		} else {
			return nil, BadRequest("canonical request tool declarations contain an unsupported kind")
		}
	}
	return tools, nil
}

func decodeRequestToolDeclsMetadataFromDTO(dto []requestToolDeclMetadataDTO) ([]ToolDecl, error) {
	if len(dto) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		return nil, InternalError("canonical request tool declarations could not be decoded")
	}
	return decodeRequestToolDeclsMetadata(string(raw))
}

func decodeToolSchemaMetadata(raw json.RawMessage) (ToolSchema, error) {
	rawText := string(raw)                // swobu:io-string source=domain
	trimmed := strings.TrimSpace(rawText) // swobu:io-string source=domain
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

func decodeToolFormatMetadata(raw json.RawMessage) (ToolFormat, error) {
	rawText := string(raw)                // swobu:io-string source=domain
	trimmed := strings.TrimSpace(rawText) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return ToolFormat{}, BadRequest("canonical request tool declarations require format")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ToolFormat{}, BadRequest("canonical request tool declarations are invalid")
	}
	return NewToolFormatObject(rawText), nil
}

func decodeToolCapabilityConfigMetadata(raw json.RawMessage) (ToolCapabilityConfig, error) {
	rawText := string(raw)                // swobu:io-string source=domain
	trimmed := strings.TrimSpace(rawText) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return EmptyToolCapabilityConfig(), nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ToolCapabilityConfig{}, BadRequest("canonical request tool capability config is invalid")
	}
	return NewToolCapabilityConfigObject(rawText), nil
}

func decodeToolPolicyMetadata(raw string) (ToolPolicy, error) {
	rawText := raw                        // swobu:io-string source=domain
	trimmed := strings.TrimSpace(rawText) // swobu:io-string source=domain
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
	specific := strings.TrimSpace(dto.Specific) // swobu:io-string source=domain
	if specific != "" {
		if mode != ToolPolicySpecific {
			return ToolPolicy{}, BadRequest("canonical request tool policy specific mode is invalid")
		}
		specificID := NewSemanticToolID(specific)
		policy := NewToolPolicy(ToolPolicySpecific, &specificID)
		policy.SpecificType = strings.ToLower(strings.TrimSpace(dto.SpecificType)) // swobu:io-string source=domain
		return policy, nil
	}
	if mode == ToolPolicySpecific {
		return ToolPolicy{}, BadRequest("canonical request tool policy specific mode requires a tool id")
	}
	return NewToolPolicy(mode, nil), nil
}
