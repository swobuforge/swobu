package httpcodec

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// DecodeJSONObject decodes an optional JSON object payload used by tool-call
// argument surfaces across OpenAI-compatible protocol families.
func DecodeJSONObject(raw json.RawMessage, message string) (map[string]any, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw))) // swobu:io-string source=boundary
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, canonical.BadRequest(message)
	}
	return out, nil
}
