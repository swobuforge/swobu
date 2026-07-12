package chatcompletions

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decodeChatCompletionsGenerationControls(dto chatCompletionsRequestDTO) (canonical.GenerationControls, error) {
	maxTokens, err := decodeOptionalIntMessage(dto.MaxTokens, "chat completions request max_tokens is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	temperature, err := decodeOptionalFloatMessage(dto.Temperature, "chat completions request temperature is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	topP, err := decodeOptionalFloatMessage(dto.TopP, "chat completions request top_p is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	stopSequences, err := decodeStopSequences(dto.Stop, "chat completions request stop is invalid")
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

func encodeChatCompletionsGenerationControls(payload map[string]any, controls canonical.GenerationControls) error {
	if value, ok := controls.Limits.MaxOutputTokens.Value(); ok {
		payload["max_tokens"] = value
	}
	if value, ok := controls.Sampling.Temperature.Value(); ok {
		payload["temperature"] = value
	}
	if value, ok := controls.Sampling.TopP.Value(); ok {
		payload["top_p"] = value
	}
	if len(controls.Limits.StopSequences) == 1 {
		payload["stop"] = controls.Limits.StopSequences[0]
		return nil
	}
	if len(controls.Limits.StopSequences) > 1 {
		payload["stop"] = append([]string(nil), controls.Limits.StopSequences...)
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

func decodeStopSequences(raw json.RawMessage, invalidMessage string) ([]string, error) {
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
