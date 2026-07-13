package messages

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decodeMessagesGenerationControls(dto messagesRequestDTO) (canonical.GenerationControls, error) {
	maxTokens, err := decodeOptionalIntMessage(dto.MaxTokens, "messages request max_tokens is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	temperature, err := decodeOptionalFloatMessage(dto.Temperature, "messages request temperature is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	topP, err := decodeOptionalFloatMessage(dto.TopP, "messages request top_p is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	stopSequences, err := decodeStopSequenceArray(dto.StopSequences, "messages request stop_sequences is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	return canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: maxTokens,
		StopSequences:   stopSequences,
		Temperature:     temperature,
		TopP:            topP,
	})
}

func encodeMessagesGenerationControls(payload map[string]any, controls canonical.GenerationControls) error {
	maxTokens := defaultMessagesMaxTokens
	if value, ok := controls.Limits.MaxOutputTokens.Value(); ok {
		maxTokens = value
	}
	payload["max_tokens"] = maxTokens
	if value, ok := controls.Sampling.Temperature.Value(); ok {
		payload["temperature"] = value
	}
	if value, ok := controls.Sampling.TopP.Value(); ok {
		payload["top_p"] = value
	}
	if len(controls.Limits.StopSequences) > 0 {
		payload["stop_sequences"] = append([]string(nil), controls.Limits.StopSequences...)
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

func decodeStopSequenceArray(raw json.RawMessage, invalidMessage string) ([]string, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, canonical.BadRequest(invalidMessage)
	}
	return values, nil
}
