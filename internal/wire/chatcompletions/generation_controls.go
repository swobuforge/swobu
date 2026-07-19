package chatcompletions

import (
	"errors"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

type MaxOutputTokensField string

const (
	MaxOutputTokensFieldLegacy     MaxOutputTokensField = "max_tokens"
	MaxOutputTokensFieldCompletion MaxOutputTokensField = "max_completion_tokens"
)

func decodeChatCompletionsGenerationControls(dto chatCompletionsRequestDTO) (canonical.GenerationControls, error) {
	maxTokens, err := decodeChatCompletionsMaxOutputTokens(dto)
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	temperature, err := openaiwire.DecodeOptionalFloat(dto.Temperature, "chat completions request temperature is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	topP, err := openaiwire.DecodeOptionalFloat(dto.TopP, "chat completions request top_p is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	stopSequences, err := openaiwire.DecodeStopSequences(dto.Stop, "chat completions request stop is invalid")
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
	maxCompletionTokens, err := openaiwire.DecodeOptionalInt(dto.MaxCompletionTokens, "chat completions request max_completion_tokens is invalid")
	if err != nil {
		return nil, err
	}
	if maxCompletionTokens != nil {
		return maxCompletionTokens, nil
	}
	return openaiwire.DecodeOptionalInt(dto.MaxTokens, "chat completions request max_tokens is invalid")
}

func encodeChatCompletionsGenerationControls(payload map[string]any, controls canonical.GenerationControls, field MaxOutputTokensField) error {
	if field != MaxOutputTokensFieldLegacy && field != MaxOutputTokensFieldCompletion {
		return errors.New("chat completions max output token field policy is required")
	}
	if value, ok := controls.Limits.MaxOutputTokens.Value(); ok {
		switch field {
		case MaxOutputTokensFieldLegacy:
			payload["max_tokens"] = value
		case MaxOutputTokensFieldCompletion:
			payload["max_completion_tokens"] = value
		}
	}
	if value, ok := controls.Sampling.Temperature.Value(); ok {
		payload["temperature"] = value
	}
	if value, ok := controls.Sampling.TopP.Value(); ok {
		payload["top_p"] = value
	}
	openaiwire.SetStopSequence(payload, "stop", controls.Limits.StopSequences)
	return nil
}
