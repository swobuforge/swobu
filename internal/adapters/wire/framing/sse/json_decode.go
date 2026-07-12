package sse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

var strictJSONUnknownFieldPattern = regexp.MustCompile(`^json: unknown field "([^"]+)"$`)

// DecodeJSONObject decodes an optional JSON object payload used by tool-call
// argument surfaces across OpenAI-style protocol families.
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

// DecodeStrictJSON decodes a JSON object and rejects unknown top-level fields.
// The returned canonical error carries a JSON-pointer detail for the offending field.
func DecodeStrictJSON(raw json.RawMessage, out any, surface string) error {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" {
		return canonical.BadRequest(surface + " body is invalid JSON")
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return strictJSONDecodeError(err, surface)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return canonical.BadRequest(surface + " body contains trailing data")
	} else if err != io.EOF {
		return canonical.BadRequest(surface + " body is invalid JSON")
	}
	return nil
}

func strictJSONDecodeError(err error, surface string) error {
	matches := strictJSONUnknownFieldPattern.FindStringSubmatch(err.Error())
	if len(matches) == 2 {
		field := matches[1]
		out := canonical.BadRequest(fmt.Sprintf("%s contains an unknown field %q", surface, field))
		out.Details = map[string]string{
			"json_pointer": "/" + escapeJSONPointer(field),
			"json_field":   field,
		}
		return out
	}
	return canonical.BadRequest(surface + " body is invalid JSON")
}

func escapeJSONPointer(part string) string {
	part = strings.ReplaceAll(part, "~", "~0")
	part = strings.ReplaceAll(part, "/", "~1")
	return part
}
