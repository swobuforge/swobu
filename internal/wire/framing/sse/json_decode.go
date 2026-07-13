package sse

import (
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

var jsonFieldSetCache sync.Map // map[reflect.Type]map[string]struct{}

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

// DecodePermissiveJSON decodes a top-level JSON object into out, logs any
// unexpected top-level fields, and ignores them.
//
// Scope stays at the request-envelope edge: malformed JSON, non-object
// payloads, trailing data, and semantic field failures still return BAD_REQUEST.
func DecodePermissiveJSON(raw json.RawMessage, out any, surface string, logger *slog.Logger) error {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" {
		return canonical.BadRequest(surface + " body is invalid JSON")
	}
	fields, err := decodeJSONObjectFields(trimmed, surface)
	if err != nil {
		return err
	}
	declared, err := declaredJSONFields(out)
	if err != nil {
		return canonical.BadRequest(surface + " body is invalid JSON")
	}
	if logger == nil {
		logger = slog.Default()
	}
	for field := range fields {
		if _, ok := declared[field]; ok {
			continue
		}
		// Visibility without mutation: unexpected top-level fields stay out of canonical state.
		logger.Warn("unexpected request field",
			"surface", surface,
			"json_field", field,
			"json_pointer", "/"+escapeJSONPointer(field),
		)
	}
	if err := json.Unmarshal([]byte(trimmed), out); err != nil {
		return canonical.BadRequest(surface + " body is invalid JSON")
	}
	return nil
}

func decodeJSONObjectFields(trimmed string, surface string) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	var out map[string]json.RawMessage
	if err := dec.Decode(&out); err != nil {
		return nil, canonical.BadRequest(surface + " body is invalid JSON")
	}
	if out == nil {
		return nil, canonical.BadRequest(surface + " body must be a JSON object")
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, canonical.BadRequest(surface + " body contains trailing data")
	} else if err != io.EOF {
		return nil, canonical.BadRequest(surface + " body is invalid JSON")
	}
	return out, nil
}

func declaredJSONFields(out any) (map[string]struct{}, error) {
	typ := reflect.TypeOf(out)
	if typ == nil || typ.Kind() != reflect.Pointer {
		return nil, canonical.BadRequest("request body target is invalid")
	}
	typ = typ.Elem()
	if typ.Kind() != reflect.Struct {
		return nil, canonical.BadRequest("request body target is invalid")
	}
	if cached, ok := jsonFieldSetCache.Load(typ); ok {
		return cached.(map[string]struct{}), nil
	}
	fields := make(map[string]struct{}, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name := field.Name
		if tag, ok := field.Tag.Lookup("json"); ok {
			if tag == "-" {
				continue
			}
			if cut, _, found := strings.Cut(tag, ","); found {
				if cut != "" {
					name = cut
				}
			} else if tag != "" {
				name = tag
			}
		}
		fields[name] = struct{}{}
	}
	jsonFieldSetCache.Store(typ, fields)
	return fields, nil
}

func escapeJSONPointer(part string) string {
	part = strings.ReplaceAll(part, "~", "~0")
	part = strings.ReplaceAll(part, "/", "~1")
	return part
}
