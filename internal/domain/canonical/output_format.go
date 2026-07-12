package canonical

import (
	"encoding/json"
	"fmt"
	"strings"
)

type OutputFormatKind string

const (
	OutputFormatUnspecified OutputFormatKind = ""
	OutputFormatText        OutputFormatKind = "text"
	OutputFormatJSONSchema  OutputFormatKind = "json_schema"
)

// OutputFormat describes the final answer format requested for one canonical
// request. Plain text remains the default; structured output is represented
// explicitly and must validate against the supported subset before adapters
// lower it.
type OutputFormat struct {
	Kind        OutputFormatKind
	Name        string
	Description string
	Schema      RawJSON
	Strict      bool
}

type OutputFormatParams struct {
	Kind        OutputFormatKind
	Name        string
	Description string
	Schema      RawJSON
	Strict      bool
}

// NewOutputFormat normalizes one canonical output-format request.
func NewOutputFormat(params OutputFormatParams) (OutputFormat, error) {
	kind := OutputFormatKind(strings.TrimSpace(string(params.Kind)))
	switch kind {
	case OutputFormatUnspecified:
		if !params.Schema.IsEmpty() || strings.TrimSpace(params.Name) != "" || strings.TrimSpace(params.Description) != "" || params.Strict {
			return OutputFormat{}, BadRequest("output format is invalid")
		}
		return OutputFormat{}, nil
	case OutputFormatText:
		if !params.Schema.IsEmpty() || strings.TrimSpace(params.Name) != "" || strings.TrimSpace(params.Description) != "" || params.Strict {
			return OutputFormat{}, BadRequest("output format text does not accept schema, description, or strict mode")
		}
		return OutputFormat{Kind: OutputFormatText}, nil
	case OutputFormatJSONSchema:
		name := strings.TrimSpace(params.Name) // swobu:io-string source=domain
		if err := validateOutputFormatName(name); err != nil {
			return OutputFormat{}, err
		}
		description := strings.TrimSpace(params.Description)
		schemaRaw := strings.TrimSpace(params.Schema.RawObject()) // swobu:io-string source=domain
		if err := validateOutputFormatSchema(schemaRaw); err != nil {
			return OutputFormat{}, err
		}
		return OutputFormat{
			Kind:        OutputFormatJSONSchema,
			Name:        name,
			Description: description,
			Schema:      NewRawJSONObject(schemaRaw),
			Strict:      params.Strict,
		}, nil
	default:
		return OutputFormat{}, BadRequest("output format kind is invalid")
	}
}

func (f OutputFormat) Clone() OutputFormat {
	return OutputFormat{
		Kind:        f.Kind,
		Name:        f.Name,
		Description: f.Description,
		Schema:      f.Schema.Clone(),
		Strict:      f.Strict,
	}
}

func (f OutputFormat) IsZero() bool {
	return strings.TrimSpace(string(f.Kind)) == "" &&
		strings.TrimSpace(f.Name) == "" &&
		strings.TrimSpace(f.Description) == "" &&
		f.Schema.IsEmpty() &&
		!f.Strict
}

func (f OutputFormat) Validate() error {
	_, err := NewOutputFormat(OutputFormatParams{
		Kind:        f.Kind,
		Name:        f.Name,
		Description: f.Description,
		Schema:      f.Schema,
		Strict:      f.Strict,
	})
	return err
}

func validateOutputFormatName(name string) error {
	if name == "" {
		return BadRequest("output format name is required")
	}
	if len(name) > 64 {
		return BadRequest("output format name is too long")
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return BadRequest("output format name contains invalid characters")
	}
	return nil
}

func validateOutputFormatSchema(raw string) error {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return BadRequest("output format schema is required")
	}
	var node any
	if err := json.Unmarshal([]byte(trimmed), &node); err != nil {
		return BadRequest("output format schema is invalid")
	}
	obj, ok := node.(map[string]any)
	if !ok {
		return BadRequest("output format schema must be a JSON object")
	}
	if err := validateOutputFormatSchemaObject(obj); err != nil {
		return err
	}
	return nil
}

// swobu:lint ignore string-switch because=JSON Schema boundary validation branches on keyword strings.
func validateOutputFormatSchemaObject(obj map[string]any) error {
	for key, value := range obj {
		switch key {
		case "type":
			if err := validateOutputFormatSchemaType(value); err != nil {
				return err
			}
		case "properties":
			props, ok := value.(map[string]any)
			if !ok {
				return BadRequest("output format schema properties must be an object")
			}
			for _, propValue := range props {
				propSchema, ok := propValue.(map[string]any)
				if !ok {
					return BadRequest("output format schema properties must contain objects")
				}
				if err := validateOutputFormatSchemaObject(propSchema); err != nil {
					return err
				}
			}
		case "required":
			if err := validateOutputFormatSchemaRequired(value); err != nil {
				return err
			}
		case "additionalProperties":
			if err := validateOutputFormatSchemaAdditionalProperties(value); err != nil {
				return err
			}
		case "items":
			itemSchema, ok := value.(map[string]any)
			if !ok {
				return BadRequest("output format schema items must be an object")
			}
			if err := validateOutputFormatSchemaObject(itemSchema); err != nil {
				return err
			}
		case "enum":
			if err := validateOutputFormatSchemaEnum(value); err != nil {
				return err
			}
		case "description":
			if _, ok := value.(string); !ok {
				return BadRequest("output format schema description must be a string")
			}
		case "oneOf", "anyOf", "allOf", "not", "patternProperties", "dependentSchemas", "$ref":
			return BadRequest(fmt.Sprintf("output format schema contains unsupported keyword %q", key))
		default:
			return BadRequest(fmt.Sprintf("output format schema contains unsupported keyword %q", key))
		}
	}
	return nil
}

func validateOutputFormatSchemaType(value any) error {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return BadRequest("output format schema type must not be empty")
		}
		return nil
	case []any:
		if len(typed) == 0 {
			return BadRequest("output format schema type must not be empty")
		}
		for _, item := range typed {
			if _, ok := item.(string); !ok {
				return BadRequest("output format schema type array must contain strings")
			}
		}
		return nil
	default:
		return BadRequest("output format schema type must be a string")
	}
}

func validateOutputFormatSchemaRequired(value any) error {
	items, ok := value.([]any)
	if !ok {
		return BadRequest("output format schema required must be an array")
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return BadRequest("output format schema required must contain strings")
		}
	}
	return nil
}

func validateOutputFormatSchemaAdditionalProperties(value any) error {
	switch typed := value.(type) {
	case bool:
		_ = typed
		return nil
	case map[string]any:
		return validateOutputFormatSchemaObject(typed)
	default:
		return BadRequest("output format schema additionalProperties must be a boolean or object")
	}
}

func validateOutputFormatSchemaEnum(value any) error {
	items, ok := value.([]any)
	if !ok {
		return BadRequest("output format schema enum must be an array")
	}
	for _, item := range items {
		switch item.(type) {
		case string, bool, float64, nil:
			continue
		default:
			return BadRequest("output format schema enum must contain primitive values")
		}
	}
	return nil
}

func decodeOutputFormatMetadata(raw string) (OutputFormat, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return OutputFormat{}, nil
	}
	type outputFormatMetadataDTO struct {
		Kind        string          `json:"kind"`
		Name        string          `json:"name,omitempty"`
		Description string          `json:"description,omitempty"`
		Schema      json.RawMessage `json:"schema,omitempty"`
		Strict      *bool           `json:"strict,omitempty"`
	}
	var dto outputFormatMetadataDTO
	if err := json.Unmarshal([]byte(trimmed), &dto); err != nil {
		return OutputFormat{}, BadRequest("canonical request output format is invalid")
	}
	strict := false
	if dto.Strict != nil {
		strict = *dto.Strict
	}
	return NewOutputFormat(OutputFormatParams{
		Kind:        OutputFormatKind(dto.Kind),
		Name:        dto.Name,
		Description: dto.Description,
		Schema:      NewRawJSONObject(string(dto.Schema)),
		Strict:      strict,
	})
}
