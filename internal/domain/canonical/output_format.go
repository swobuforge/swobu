package canonical

import (
	"encoding/json"
	"strings"
)

type OutputFormatKind string

const (
	OutputFormatUnspecified OutputFormatKind = ""
	OutputFormatText        OutputFormatKind = "text"
	OutputFormatJSONObject  OutputFormatKind = "json_object"
	OutputFormatJSONSchema  OutputFormatKind = "json_schema"
)

// OutputFormat describes the final answer format requested for one canonical
// request. Plain text remains the default. Structured output preserves a valid
// JSON-object schema opaquely; provider adapters, not canonical admission, own
// support for particular JSON Schema dialects and keywords.
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
	kind := OutputFormatKind(strings.TrimSpace(string(params.Kind))) // swobu:io-string source=domain
	switch kind {
	case OutputFormatUnspecified:
		if !params.Schema.IsEmpty() || strings.TrimSpace(params.Name) != "" || strings.TrimSpace(params.Description) != "" || params.Strict { // swobu:io-string source=domain
			return OutputFormat{}, BadRequest("output format is invalid")
		}
		return OutputFormat{}, nil
	case OutputFormatText:
		if !params.Schema.IsEmpty() || strings.TrimSpace(params.Name) != "" || strings.TrimSpace(params.Description) != "" || params.Strict { // swobu:io-string source=domain
			return OutputFormat{}, BadRequest("output format text does not accept schema, description, or strict mode")
		}
		return OutputFormat{Kind: OutputFormatText}, nil
	case OutputFormatJSONObject:
		if !params.Schema.IsEmpty() || strings.TrimSpace(params.Name) != "" || strings.TrimSpace(params.Description) != "" || params.Strict { // swobu:io-string source=domain
			return OutputFormat{}, BadRequest("output format json_object does not accept schema, description, or strict mode")
		}
		return OutputFormat{Kind: OutputFormatJSONObject}, nil
	case OutputFormatJSONSchema:
		name := strings.TrimSpace(params.Name) // swobu:io-string source=domain
		if err := validateOutputFormatName(name); err != nil {
			return OutputFormat{}, err
		}
		description := strings.TrimSpace(params.Description)      // swobu:io-string source=domain
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
	return strings.TrimSpace(string(f.Kind)) == "" && // swobu:io-string source=domain
		strings.TrimSpace(f.Name) == "" && // swobu:io-string source=domain
		strings.TrimSpace(f.Description) == "" && // swobu:io-string source=domain
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
	_, ok := node.(map[string]any)
	if !ok {
		return BadRequest("output format schema must be a JSON object")
	}
	return nil
}
