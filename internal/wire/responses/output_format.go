package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
)

func decodeResponsesOutputFormat(text *responsesTextDTO, changeLog *[]compat.Change, exchangeID string) (canonical.OutputFormat, error) {
	if text == nil {
		return canonical.OutputFormat{}, nil
	}
	formatType := strings.TrimSpace(text.Format.Type) // swobu:io-string source=boundary
	if formatType == "" {
		if strings.TrimSpace(text.Format.Name) == "" && strings.TrimSpace(text.Format.Description) == "" && len(text.Format.Schema) == 0 && text.Format.Strict == nil { // swobu:io-string source=domain
			return canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatText})
		}
		return canonical.OutputFormat{}, canonical.BadRequest("responses request text.format is invalid")
	}
	switch canonical.OutputFormatKind(formatType) {
	case canonical.OutputFormatText:
		return canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatText})
	case canonical.OutputFormatJSONObject:
		return canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONObject})
	case canonical.OutputFormatJSONSchema:
		strict := false
		if text.Format.Strict != nil {
			strict = *text.Format.Strict
		}
		return canonical.NewOutputFormat(canonical.OutputFormatParams{
			Kind:        canonical.OutputFormatJSONSchema,
			Name:        text.Format.Name,
			Description: text.Format.Description,
			Schema:      canonical.NewRawJSONObject(string(text.Format.Schema)),
			Strict:      strict,
		})
	default:
		return canonical.OutputFormat{}, wire.RejectUnknownOutputFormat("Responses", "text.format type "+formatType)
	}
}

func encodeResponsesOutputFormat(format canonical.OutputFormat) (*responsesTextDTO, error) {
	if format.IsZero() {
		return nil, nil
	}
	if err := format.Validate(); err != nil {
		return nil, err
	}
	switch format.Kind {
	case canonical.OutputFormatText:
		return &responsesTextDTO{
			Format: responsesTextFormatDTO{Type: string(canonical.OutputFormatText)},
		}, nil
	case canonical.OutputFormatJSONObject:
		return &responsesTextDTO{
			Format: responsesTextFormatDTO{Type: string(canonical.OutputFormatJSONObject)},
		}, nil
	case canonical.OutputFormatJSONSchema:
		dto := responsesTextFormatDTO{
			Type:        string(canonical.OutputFormatJSONSchema),
			Name:        strings.TrimSpace(format.Name),        // swobu:io-string source=domain
			Description: strings.TrimSpace(format.Description), // swobu:io-string source=domain
		}
		if !format.Schema.IsEmpty() {
			dto.Schema = json.RawMessage(format.Schema.RawObject())
		}
		if format.Strict {
			strict := true
			dto.Strict = &strict
		}
		return &responsesTextDTO{Format: dto}, nil
	default:
		return nil, canonical.InternalError("canonical output format kind is invalid")
	}
}
