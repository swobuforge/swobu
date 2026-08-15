package openai

import (
	"encoding/json"
	"io"
	"slices"
	"strings"
)

// ModelRow is one non-empty OpenAI catalog row. RawJSON preserves the
// provider-owned row fields for an authoring-only projector; those fields must
// never become request-path capability authority.
type ModelRow struct {
	id  string
	raw json.RawMessage
}

// ID returns the exact model identity advertised by the catalog row.
func (r ModelRow) ID() string { return r.id }

// RawJSON returns a defensive copy of the original catalog row.
func (r ModelRow) RawJSON() []byte { return slices.Clone(r.raw) }

// DecodeModelRows decodes OpenAI-style model catalog payloads with
// `data[].id` entries while preserving each complete row for optional
// authoring-only projection.
func DecodeModelRows(respBody io.Reader) ([]ModelRow, error) {
	var payload struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(respBody).Decode(&payload); err != nil {
		return nil, err
	}
	rows := make([]ModelRow, 0, len(payload.Data))
	for _, raw := range payload.Data {
		var model struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &model); err != nil {
			return nil, err
		}
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		rows = append(rows, ModelRow{id: id, raw: slices.Clone(raw)})
	}
	return rows, nil
}

// DecodeModelIDs decodes OpenAI-style model catalog payloads with
// `data[].id` entries.
func DecodeModelIDs(respBody io.Reader) ([]string, error) {
	rows, err := DecodeModelRows(respBody)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(rows))
	for _, row := range rows {
		models = append(models, row.ID())
	}
	slices.Sort(models)
	return slices.Compact(models), nil
}
