package messages

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
)

type messagesResponseFormatDTO struct {
	Type       string                          `json:"type"`
	JSONSchema *messagesJSONSchemaOutputFormat `json:"json_schema,omitempty"`
}

type messagesJSONSchemaOutputFormat struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

func decodeMessagesOutputFormat(raw json.RawMessage, changeLog *[]compat.Change, exchangeID string) (canonical.OutputFormat, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return canonical.OutputFormat{}, nil
	}
	var dto messagesResponseFormatDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return canonical.OutputFormat{}, canonical.BadRequest("messages request response_format is invalid")
	}
	switch strings.TrimSpace(dto.Type) { // swobu:io-string source=boundary
	case "":
		return canonical.OutputFormat{}, canonical.BadRequest("messages request response_format is invalid")
	case "json_object":
		return canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONObject})
	case "json_schema":
		if dto.JSONSchema == nil {
			return canonical.OutputFormat{}, canonical.BadRequest("messages request response_format json_schema is required")
		}
		conformance := canonical.SchemaConformanceDefault
		if dto.JSONSchema.Strict != nil {
			if *dto.JSONSchema.Strict {
				conformance = canonical.SchemaConformanceEnforced
			} else {
				conformance = canonical.SchemaConformanceRelaxed
			}
		}
		schema := canonical.NewRawJSONObject(string(dto.JSONSchema.Schema))
		return canonical.NewOutputFormat(canonical.OutputFormatParams{
			Kind: canonical.OutputFormatJSONSchema, Name: dto.JSONSchema.Name, Description: dto.JSONSchema.Description, Schema: schema,
			SchemaContract: canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled, Conformance: conformance},
		})
	default:
		return canonical.OutputFormat{}, wire.RejectUnknownOutputFormat("Messages", "response_format type "+strings.TrimSpace(dto.Type))
	}
}

func encodeMessagesOutputFormat(format canonical.OutputFormat, changeLog *[]compat.Change) (json.RawMessage, error) {
	if format.IsZero() || format.Kind == canonical.OutputFormatText {
		return nil, nil
	}
	if err := format.Validate(); err != nil {
		return nil, err
	}
	switch format.Kind {
	case canonical.OutputFormatJSONObject:
		if changeLog != nil {
			*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestOutputFormat, canonical.Occurrence{}))
		}
		return nil, nil
	case canonical.OutputFormatJSONSchema:
		if (!wire.SchemaContractExact(format.SchemaContract, canonical.SchemaProfileAnthropic) || format.Conformance() != canonical.SchemaConformanceEnforced) && changeLog != nil {
			*changeLog = compat.AppendUnique(*changeLog, compat.NewApproximation(canonical.RequestOutputSchemaConformance, canonical.Occurrence{}))
		}
		dto := messagesNativeOutputFormatDTO{
			Type:   "json_schema",
			Schema: json.RawMessage(format.Schema.RawObject()),
		}
		raw, err := json.Marshal(dto)
		if err != nil {
			return nil, canonical.InternalError("messages output format could not be encoded")
		}
		return raw, nil
	default:
		return nil, canonical.InternalError("canonical output format kind is invalid")
	}
}

func decodeMessagesNativeOutputFormat(format *messagesNativeOutputFormatDTO, changeLog *[]compat.Change, exchangeID string) (canonical.OutputFormat, error) {
	if format == nil {
		return canonical.OutputFormat{}, nil
	}
	formatType := strings.TrimSpace(format.Type) // swobu:io-string source=boundary
	if formatType == "" {
		return canonical.OutputFormat{}, canonical.BadRequest("messages request output_config format is invalid")
	}
	if formatType != "json_schema" {
		return canonical.OutputFormat{}, wire.RejectUnknownOutputFormat("Messages", "output_config format type "+formatType)
	}
	return canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:           canonical.OutputFormatJSONSchema,
		Schema:         canonical.NewRawJSONObject(string(format.Schema)),
		SchemaContract: canonical.SchemaContract{Profile: canonical.SchemaProfileAnthropic, Conformance: canonical.SchemaConformanceEnforced},
	})
}

// OmitOutputFormat drops structured output for a Messages target that cannot accept it.
func OmitOutputFormat(format canonical.OutputFormat, changes *[]compat.Change) (json.RawMessage, error) {
	if format.IsZero() || format.Kind == canonical.OutputFormatText {
		return nil, nil
	}
	if changes != nil {
		*changes = compat.AppendUnique(*changes, compat.NewOmission(canonical.RequestOutputFormat, canonical.Occurrence{}))
	}
	return nil, nil
}
