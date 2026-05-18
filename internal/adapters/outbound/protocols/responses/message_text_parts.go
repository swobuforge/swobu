package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decodeMessageTextParts(raw json.RawMessage) ([]canonical.CanonicalItem, error) {
	var content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, canonical.InternalError("responses message content is invalid")
	}
	parts := make([]canonical.CanonicalItem, 0, len(content))
	for _, part := range content {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary
		switch partType {
		case "text", "output_text", "input_text":
			parts = append(parts, canonical.NewTextItem(canonical.ItemAuthorAssistant, part.Text))
		default:
			return nil, canonical.UnsupportedOperation("responses message content part type is not implemented")
		}
	}
	return parts, nil
}
