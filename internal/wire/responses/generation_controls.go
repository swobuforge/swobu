package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
	"github.com/swobuforge/swobu/internal/wire/reasoningprojection"
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
	var effort *canonical.InferenceEffort
	if dto.Reasoning != nil && strings.TrimSpace(dto.Reasoning.Effort) != "" && strings.TrimSpace(dto.Reasoning.Effort) != "none" { // swobu:io-string source=boundary
		switch raw := strings.TrimSpace(dto.Reasoning.Effort); raw { // swobu:io-string source=boundary
		case "minimal", "low", "medium", "high", "xhigh", "max":
			value := canonical.InferenceEffort(raw)
			effort = &value
		}
	}
	return canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: maxOutputTokens,
		StopSequences:   stopSequences,
		Temperature:     temperature,
		TopP:            topP,
		Effort:          effort,
	})
}

func decodeResponsesReasoning(dto *responsesReasoningRequestDTO, includeRaw json.RawMessage, changeLog *[]compat.Change, exchangeID string) (canonical.ReasoningControls, error) {
	params := canonical.ReasoningControlsParams{}
	if dto != nil {
		switch value := strings.TrimSpace(dto.Effort); value { // swobu:io-string source=boundary
		case "":
		case "none":
			params.Compute = canonical.Specify(canonical.NewDisabledReasoningCompute())
		case "minimal", "low", "medium", "high", "xhigh", "max":
			params.Compute = canonical.Specify(canonical.NewAutomaticReasoningCompute())
		default:
			if err := appendResponsesOccurrenceChange(changeLog, exchangeID, canonical.RequestReasoning, compat.Approximation, canonical.Occurrence{}); err != nil {
				return canonical.ReasoningControls{}, err
			}
		}
		switch value := strings.TrimSpace(dto.Summary); value { // swobu:io-string source=boundary
		case "":
		case "concise", "detailed", "auto":
			params.Disclosure = canonical.Specify(canonical.ReasoningDisclosureSummary)
		default:
			if err := appendResponsesOccurrenceChange(changeLog, exchangeID, canonical.RequestReasoning, compat.Approximation, canonical.Occurrence{}); err != nil {
				return canonical.ReasoningControls{}, err
			}
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
	includeEncrypted, err := decodeResponsesReasoningInclude(includeRaw, changeLog, exchangeID)
	if err != nil {
		return canonical.ReasoningControls{}, err
	}
	_ = includeEncrypted // capture policy is Responses-native, not readable canonical disclosure
	return canonical.NewReasoningControls(params)
}

func decodeResponsesReasoningInclude(raw json.RawMessage, changeLog *[]compat.Change, exchangeID string) (bool, error) {
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
			continue
		}
		includeEncrypted = true
	}
	return includeEncrypted, nil
}

func encodeResponsesReasoning(payload map[string]any, reasoning canonical.ReasoningControls, effortField canonical.Specified[canonical.InferenceEffort], acceptsEffortMax, acceptsDisabled func() bool, changeLog *[]compat.Change) error {
	wireReasoning := map[string]any{}
	if compute, ok := reasoning.ComputeField().Get(); ok {
		if disclosure, disclosed := reasoning.DisclosureField().Get(); disclosed && compute.Kind() == canonical.ReasoningDisabled && disclosure != canonical.ReasoningDisclosureNone {
			return canonical.InternalError("disabled reasoning carries readable disclosure")
		}
	}

	projection := reasoningprojection.ProjectOrdinalReasoning(reasoning, effortField)
	switch projection.Kind {
	case reasoningprojection.OrdinalDisabled:
		if acceptsDisabled == nil || acceptsDisabled() {
			wireReasoning["effort"] = "none"
		} else if changeLog != nil {
			*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestReasoning, canonical.Occurrence{}))
		}
	case reasoningprojection.OrdinalEffort:
		effort := projection.Effort
		if effort == canonical.InferenceEffortMax && acceptsEffortMax != nil && !acceptsEffortMax() {
			effort = canonical.InferenceEffortXHigh
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewApproximation(canonical.RequestControlsEffort, canonical.Occurrence{}))
			}
		}
		wireReasoning["effort"] = string(effort)
	}
	if changeLog != nil {
		for _, change := range projection.Changes {
			*changeLog = compat.AppendUnique(*changeLog, change)
		}
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

func encodeResponsesGenerationControls(payload map[string]any, controls canonical.GenerationControls, omitMaxOutputTokens bool, changeLog *[]compat.Change) {
	if value, ok := controls.Limits.MaxOutputTokens.Value(); ok {
		if omitMaxOutputTokens {
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestControlsMaxOutputTokens, canonical.Occurrence{}))
			}
		} else {
			payload["max_output_tokens"] = value
		}
	}
	if value, ok := controls.Sampling.Temperature.Value(); ok {
		payload["temperature"] = value
	}
	if value, ok := controls.Sampling.TopP.Value(); ok {
		payload["top_p"] = value
	}
	if len(controls.Limits.StopSequences) > 0 {
		if changeLog != nil {
			*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestControlsStopSequences, canonical.Occurrence{}))
		}
	}
}

func isRawControlSet(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	return trimmed != "" && trimmed != "null"
}
