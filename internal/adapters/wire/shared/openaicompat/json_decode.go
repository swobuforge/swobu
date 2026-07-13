package openaicompat

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// SetIfPresent writes val into payload[key] only when ok is true.
// Keeps field-name policy in the caller; this helper owns only the
// mechanical "set if present" pattern.
func SetIfPresent[T any](payload map[string]any, key string, val T, ok bool) {
	if ok {
		payload[key] = val
	}
}

// SetStopSequence writes a stop-sequence list into payload.
// Single element → string; multiple → []string copy. Empty → no-op.
func SetStopSequence(payload map[string]any, key string, stop []string) {
	if len(stop) == 0 {
		return
	}
	if len(stop) == 1 {
		payload[key] = stop[0]
		return
	}
	payload[key] = append([]string(nil), stop...)
}

// DecodeOptionalInt unmarshals an optional JSON integer.
// Null, empty, or missing → nil. Invalid → BadRequest.
func DecodeOptionalInt(raw json.RawMessage, invalidMessage string) (*int, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, canonical.BadRequest(invalidMessage)
	}
	return &value, nil
}

// DecodeOptionalFloat unmarshals an optional JSON float.
// Null, empty, or missing → nil. Invalid → BadRequest.
func DecodeOptionalFloat(raw json.RawMessage, invalidMessage string) (*float64, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, canonical.BadRequest(invalidMessage)
	}
	return &value, nil
}

// DecodeStopSequences unmarshals a JSON string or string-array into a slice.
// Null, empty, or missing → nil. Invalid → BadRequest.
func DecodeStopSequences(raw json.RawMessage, invalidMessage string) ([]string, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}, nil
	}
	var multiple []string
	if err := json.Unmarshal(raw, &multiple); err != nil {
		return nil, canonical.BadRequest(invalidMessage)
	}
	return multiple, nil
}
