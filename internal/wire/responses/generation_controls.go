package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decodeResponsesGenerationControls(dto responsesRequestDTO) (canonical.GenerationControls, error) {
	maxOutputTokens, err := decodeOptionalIntMessage(dto.MaxOutputTokens, "responses request max_output_tokens is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	temperature, err := decodeOptionalFloatMessage(dto.Temperature, "responses request temperature is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	topP, err := decodeOptionalFloatMessage(dto.TopP, "responses request top_p is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	if isRawControlSet(dto.Stop) {
		// Responses v0 does not carry stop sequences in canonical form; fail
		// closed rather than silently dropping supported user intent.
		return canonical.GenerationControls{}, canonical.UnsupportedOperation("responses protocol does not support stop sequences on swobu v0")
	}
	return canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: maxOutputTokens,
		Temperature:     temperature,
		TopP:            topP,
	})
}

func encodeResponsesGenerationControls(payload map[string]any, controls canonical.GenerationControls) error {
	if value, ok := controls.Limits.MaxOutputTokens.Value(); ok {
		payload["max_output_tokens"] = value
	}
	if value, ok := controls.Sampling.Temperature.Value(); ok {
		payload["temperature"] = value
	}
	if value, ok := controls.Sampling.TopP.Value(); ok {
		payload["top_p"] = value
	}
	if len(controls.Limits.StopSequences) > 0 {
		return canonical.UnsupportedOperation("responses protocol does not support stop sequences on swobu v0")
	}
	return nil
}

func decodeOptionalIntMessage(raw json.RawMessage, invalidMessage string) (*int, error) {
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

func decodeOptionalFloatMessage(raw json.RawMessage, invalidMessage string) (*float64, error) {
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

func isRawControlSet(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	return trimmed != "" && trimmed != "null"
}
