package core

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// DecodeRequestStreamFlag extracts optional request stream mode from raw JSON.
// Missing field means buffered delivery.
func DecodeRequestStreamFlag(raw []byte, protocolLabel string) (bool, error) {
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false, canonical.BadRequest(protocolLabel + " request body is invalid JSON")
	}
	value, ok := payload["stream"]
	if !ok {
		return false, nil
	}
	enabled, ok := value.(bool)
	if !ok {
		return false, canonical.BadRequest(protocolLabel + " request stream field must be a boolean")
	}
	return enabled, nil
}
