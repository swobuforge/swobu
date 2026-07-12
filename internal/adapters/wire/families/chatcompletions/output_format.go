package chatcompletions

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type chatCompletionsResponseFormatDTO struct {
	Type       string                              `json:"type"`
	JSONSchema *chatCompletionsJSONSchemaFormatDTO `json:"json_schema,omitempty"`
}

type chatCompletionsJSONSchemaFormatDTO struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

func decodeChatCompletionsOutputFormat(raw json.RawMessage) (canonical.OutputFormat, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return canonical.OutputFormat{}, nil
	}
	var dto chatCompletionsResponseFormatDTO
	if err := json.Unmarshal([]byte(trimmed), &dto); err != nil {
		return canonical.OutputFormat{}, canonical.BadRequest("chat completions request response_format is invalid")
	}
	switch strings.TrimSpace(dto.Type) {
	case "":
		return canonical.OutputFormat{}, canonical.BadRequest("chat completions request response_format is invalid")
	case "json_schema":
		if dto.JSONSchema == nil {
			return canonical.OutputFormat{}, canonical.BadRequest("chat completions request response_format json_schema is required")
		}
		strict := false
		if dto.JSONSchema.Strict != nil {
			strict = *dto.JSONSchema.Strict
		}
		return canonical.NewOutputFormat(canonical.OutputFormatParams{
			Kind:        canonical.OutputFormatJSONSchema,
			Name:        dto.JSONSchema.Name,
			Description: dto.JSONSchema.Description,
			Schema:      canonical.NewRawJSONObject(string(dto.JSONSchema.Schema)),
			Strict:      strict,
		})
	case "json_object":
		return canonical.OutputFormat{}, canonical.UnsupportedOperation("chat completions request json_object response_format is not supported on swobu v0")
	default:
		return canonical.OutputFormat{}, canonical.UnsupportedOperation("chat completions request response_format type is not supported")
	}
}

func encodeChatCompletionsOutputFormat(format canonical.OutputFormat) (json.RawMessage, error) {
	if format.IsZero() {
		return nil, nil
	}
	if err := format.Validate(); err != nil {
		return nil, err
	}
	if format.Kind == canonical.OutputFormatText {
		return nil, nil
	}
	if format.Kind != canonical.OutputFormatJSONSchema {
		return nil, canonical.UnsupportedOperation("chat completions protocol does not support the requested output format")
	}
	dto := chatCompletionsResponseFormatDTO{
		Type: string(canonical.OutputFormatJSONSchema),
		JSONSchema: &chatCompletionsJSONSchemaFormatDTO{
			Name:        strings.TrimSpace(format.Name),
			Description: strings.TrimSpace(format.Description),
			Schema:      json.RawMessage(format.Schema.RawObject()),
		},
	}
	if format.Strict {
		strict := true
		dto.JSONSchema.Strict = &strict
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		return nil, canonical.InternalError("chat completions request output format could not be encoded")
	}
	return raw, nil
}
