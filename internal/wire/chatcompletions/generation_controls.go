package chatcompletions

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

func decodeChatCompletionsGenerationControls(dto chatCompletionsRequestDTO) (canonical.GenerationControls, canonical.ReasoningControls, error) {
	maxTokens, err := decodeChatCompletionsMaxOutputTokens(dto)
	if err != nil {
		return canonical.GenerationControls{}, canonical.ReasoningControls{}, err
	}
	temperature, err := openaiwire.DecodeOptionalFloat(dto.Temperature, "chat completions request temperature is invalid")
	if err != nil {
		return canonical.GenerationControls{}, canonical.ReasoningControls{}, err
	}
	topP, err := openaiwire.DecodeOptionalFloat(dto.TopP, "chat completions request top_p is invalid")
	if err != nil {
		return canonical.GenerationControls{}, canonical.ReasoningControls{}, err
	}
	stopSequences, err := openaiwire.DecodeStopSequences(dto.Stop, "chat completions request stop is invalid")
	if err != nil {
		return canonical.GenerationControls{}, canonical.ReasoningControls{}, err
	}
	var effort *canonical.InferenceEffort
	reasoning := canonical.ReasoningControls{}
	if len(dto.ReasoningEffort) > 0 {
		var value string
		if err := json.Unmarshal(dto.ReasoningEffort, &value); err != nil {
			return canonical.GenerationControls{}, canonical.ReasoningControls{}, canonical.BadRequest("chat completions request reasoning_effort is invalid")
		}
		value = strings.TrimSpace(value) // swobu:io-string source=boundary
		if value == "none" {
			var err error
			reasoning, err = canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(canonical.NewDisabledReasoningCompute())})
			if err != nil {
				return canonical.GenerationControls{}, canonical.ReasoningControls{}, err
			}
		} else {
			parsed := canonical.InferenceEffort(value)
			effort = &parsed
		}
	}
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: maxTokens,
		StopSequences:   stopSequences,
		Temperature:     temperature,
		TopP:            topP,
		Effort:          effort,
	})
	return controls, reasoning, err
}

func encodeChatCompletionsReasoning(payload map[string]any, request canonical.CanonicalRequest) error {
	if compute, ok := request.Reasoning().ComputeField().Get(); ok {
		switch compute.Kind() {
		case canonical.ReasoningDisabled:
			if request.Controls().Effort.IsSpecified() {
				return provider.NewIncompatibleTarget("Chat Completions cannot represent disabled reasoning with inference effort")
			}
			payload["reasoning_effort"] = "none"
		case canonical.ReasoningAutomatic, canonical.ReasoningBudget:
			return provider.NewIncompatibleTarget("Chat Completions reasoning_effort cannot represent explicit canonical reasoning compute")
		}
	}
	if effort, ok := request.Controls().Effort.Get(); ok {
		payload["reasoning_effort"] = effort
	}
	return nil
}

func decodeChatCompletionsMaxOutputTokens(dto chatCompletionsRequestDTO) (*int, error) {
	// GPT-5-series reasoning models use the newer field name on chat completions.
	// Prefer it when present, then fall back to max_tokens for compatible backends.
	maxCompletionTokens, err := openaiwire.DecodeOptionalInt(dto.MaxCompletionTokens, "chat completions request max_completion_tokens is invalid")
	if err != nil {
		return nil, err
	}
	if maxCompletionTokens != nil {
		return maxCompletionTokens, nil
	}
	return openaiwire.DecodeOptionalInt(dto.MaxTokens, "chat completions request max_tokens is invalid")
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
	openaiwire.SetStopSequence(payload, "stop", controls.Limits.StopSequences)
	return nil
}
