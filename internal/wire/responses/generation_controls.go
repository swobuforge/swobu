package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

func decodeResponsesGenerationControls(dto responsesRequestDTO) (canonical.GenerationControls, error) {
	maxOutputTokens, err := openaiwire.DecodeOptionalInt(dto.MaxOutputTokens, "responses request max_output_tokens is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	temperature, err := openaiwire.DecodeOptionalFloat(dto.Temperature, "responses request temperature is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	topP, err := openaiwire.DecodeOptionalFloat(dto.TopP, "responses request top_p is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	stopSequences, err := openaiwire.DecodeStopSequences(dto.Stop, "responses request stop is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	if len(stopSequences) > 0 {
		// Responses v0 does not carry stop sequences in canonical form; fail
		// closed rather than silently dropping supported user intent.
		return canonical.GenerationControls{}, canonical.NotImplemented("Swobu cannot yet preserve Responses stop sequences")
	}
	var effort *canonical.InferenceEffort
	if dto.Reasoning != nil && strings.TrimSpace(dto.Reasoning.Effort) != "" && strings.TrimSpace(dto.Reasoning.Effort) != "none" { // swobu:io-string source=boundary
		value := canonical.InferenceEffort(strings.TrimSpace(dto.Reasoning.Effort)) // swobu:io-string source=boundary
		effort = &value
	}
	return canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: maxOutputTokens,
		StopSequences:   stopSequences,
		Temperature:     temperature,
		TopP:            topP,
		Effort:          effort,
	})
}

func decodeResponsesReasoning(dto *responsesReasoningRequestDTO, includeRaw json.RawMessage) (canonical.ReasoningControls, error) {
	params := canonical.ReasoningControlsParams{}
	if dto != nil {
		switch value := strings.TrimSpace(dto.Effort); value { // swobu:io-string source=boundary
		case "":
		case "none":
			params.Compute = canonical.Specify(canonical.NewDisabledReasoningCompute())
		case "minimal", "low", "medium", "high", "xhigh", "max":
			params.Compute = canonical.Specify(canonical.NewAutomaticReasoningCompute())
		default:
			return canonical.ReasoningControls{}, canonical.BadRequest("responses reasoning effort is invalid")
		}
		switch value := strings.TrimSpace(dto.Summary); value { // swobu:io-string source=boundary
		case "":
		case "concise", "detailed", "auto":
			params.Disclosure = canonical.Specify(canonical.ReasoningDisclosureSummary)
		default:
			return canonical.ReasoningControls{}, canonical.BadRequest("responses reasoning summary is invalid")
		}
		if isRawControlSet(dto.Context) {
			var rawContext string
			if err := json.Unmarshal(dto.Context, &rawContext); err != nil {
				return canonical.ReasoningControls{}, canonical.BadRequest("responses reasoning context is invalid")
			}
			contextValue := canonical.ResponsesReasoningContext(rawContext)
			params.ResponsesContext = canonical.Specify(contextValue)
		}
	}
	includeEncrypted, err := decodeResponsesReasoningInclude(includeRaw)
	if err != nil {
		return canonical.ReasoningControls{}, err
	}
	_ = includeEncrypted // capture policy is Responses-native, not readable canonical disclosure
	return canonical.NewReasoningControls(params)
}

func decodeResponsesReasoningInclude(raw json.RawMessage) (bool, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=provider-wire
	if trimmed == "" || trimmed == "null" {
		return false, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return false, canonical.BadRequest("responses include is invalid")
	}
	includeEncrypted := false
	for _, value := range values {
		if value == "web_search_call.action.sources" {
			continue
		}
		if value != "reasoning.encrypted_content" {
			return false, canonical.NotImplemented("Swobu cannot yet project this Responses include entry")
		}
		includeEncrypted = true
	}
	return includeEncrypted, nil
}

func encodeResponsesReasoning(payload map[string]any, reasoning canonical.ReasoningControls, effortField canonical.Specified[canonical.InferenceEffort]) error {
	wireReasoning := map[string]any{}
	if compute, ok := reasoning.ComputeField().Get(); ok {
		if disclosure, disclosed := reasoning.DisclosureField().Get(); disclosed && compute.Kind() == canonical.ReasoningDisabled && disclosure != canonical.ReasoningDisclosureNone {
			return provider.NewIncompatibleTarget("Responses cannot represent disabled reasoning with readable disclosure")
		}
		switch compute.Kind() {
		case canonical.ReasoningDisabled:
			wireReasoning["effort"] = "none"
		case canonical.ReasoningAutomatic:
			if !effortField.IsSpecified() {
				return provider.NewIncompatibleTarget("Responses target has no proof that omitted effort enables dynamic reasoning")
			}
		case canonical.ReasoningBudget:
			return provider.NewIncompatibleTarget("Responses cannot represent a numeric reasoning budget")
		}
	}
	if effort, ok := effortField.Get(); ok {
		if existing, disabled := wireReasoning["effort"]; disabled && existing == "none" {
			return canonical.BadRequest("disabled reasoning conflicts with inference effort")
		}
		wireReasoning["effort"] = effort
	}
	if disclosure, ok := reasoning.DisclosureField().Get(); ok && disclosure == canonical.ReasoningDisclosureSummary {
		wireReasoning["summary"] = "auto"
	}
	if contextValue, ok := reasoning.ResponsesContextField().Get(); ok {
		wireReasoning["context"] = contextValue
	}
	if len(wireReasoning) > 0 {
		payload["reasoning"] = wireReasoning
	}
	return nil
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
		return provider.NewIncompatibleTarget("Responses cannot represent canonical stop sequences")
	}
	return nil
}

func isRawControlSet(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	return trimmed != "" && trimmed != "null"
}
