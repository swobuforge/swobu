package chatcompletions

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
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

// swobu:lint ignore string-switch because=protocol boundary decodes response_format variants.
func decodeChatCompletionsOutputFormat(raw json.RawMessage, changeLog *[]compat.Change, exchangeID string) (canonical.OutputFormat, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return canonical.OutputFormat{}, nil
	}
	var dto chatCompletionsResponseFormatDTO
	if err := json.Unmarshal([]byte(trimmed), &dto); err != nil {
		return canonical.OutputFormat{}, canonical.BadRequest("chat completions request response_format is invalid")
	}
	switch strings.TrimSpace(dto.Type) { // swobu:io-string source=domain
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
		return canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONObject})
	default:
		return canonical.OutputFormat{}, wire.RejectUnknownOutputFormat("Chat Completions", "response_format type "+strings.TrimSpace(dto.Type))
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
	if format.Kind == canonical.OutputFormatJSONObject {
		return json.RawMessage(`{"type":"json_object"}`), nil
	}
	if format.Kind != canonical.OutputFormatJSONSchema {
		return nil, provider.IncompatibleCapability(canonical.RequestOutputFormat, canonical.Occurrence{}, "Chat Completions cannot represent the canonical output format")
	}
	dto := chatCompletionsResponseFormatDTO{
		Type: string(canonical.OutputFormatJSONSchema),
		JSONSchema: &chatCompletionsJSONSchemaFormatDTO{
			Name:        strings.TrimSpace(format.Name),        // swobu:io-string source=domain
			Description: strings.TrimSpace(format.Description), // swobu:io-string source=domain
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
