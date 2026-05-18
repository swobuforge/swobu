package openaicompat

import (
	"encoding/json"
	"io"
	"slices"
	"strings"
)

// DecodeModelIDs decodes OpenAI-compatible model catalog payloads with
// `data[].id` entries.
func DecodeModelIDs(respBody io.Reader) ([]string, error) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(respBody).Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(payload.Data))
	for _, model := range payload.Data {
		id := strings.TrimSpace(model.ID) // swobu:io-string source=boundary
		if id == "" {
			continue
		}
		models = append(models, id)
	}
	slices.Sort(models)
	return slices.Compact(models), nil
}
