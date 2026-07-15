package chatcompletions

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaicompat "github.com/swobuforge/swobu/internal/wire/openai"
)

func decodeChatCompletionsGenerationControls(dto chatCompletionsRequestDTO) (canonical.GenerationControls, error) {
	maxTokens, err := decodeChatCompletionsMaxOutputTokens(dto)
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	temperature, err := openaicompat.DecodeOptionalFloat(dto.Temperature, "chat completions request temperature is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	topP, err := openaicompat.DecodeOptionalFloat(dto.TopP, "chat completions request top_p is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	stopSequences, err := openaicompat.DecodeStopSequences(dto.Stop, "chat completions request stop is invalid")
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

func decodeChatCompletionsMaxOutputTokens(dto chatCompletionsRequestDTO) (*int, error) {
	// GPT-5-series reasoning models use the newer field name on chat completions.
	// Prefer it when present, then fall back to the legacy name for older models.
	maxCompletionTokens, err := openaicompat.DecodeOptionalInt(dto.MaxCompletionTokens, "chat completions request max_completion_tokens is invalid")
	if err != nil {
		return nil, err
	}
	if maxCompletionTokens != nil {
		return maxCompletionTokens, nil
	}
	return openaicompat.DecodeOptionalInt(dto.MaxTokens, "chat completions request max_tokens is invalid")
}

func encodeChatCompletionsGenerationControls(payload map[string]any, _ string, controls canonical.GenerationControls) error {
	if value, ok := controls.Limits.MaxOutputTokens.Value(); ok {
		payload["max_completion_tokens"] = value
	}
	if value, ok := controls.Sampling.Temperature.Value(); ok {
		payload["temperature"] = value
	}
	if value, ok := controls.Sampling.TopP.Value(); ok {
		payload["top_p"] = value
	}
	openaicompat.SetStopSequence(payload, "stop", controls.Limits.StopSequences)
	return nil
}
