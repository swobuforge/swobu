package messages

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

func decodeMessagesGenerationControls(dto messagesRequestDTO) (canonical.GenerationControls, error) {
	maxTokens, err := openaiwire.DecodeOptionalInt(dto.MaxTokens, "messages request max_tokens is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	temperature, err := openaiwire.DecodeOptionalFloat(dto.Temperature, "messages request temperature is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	topP, err := openaiwire.DecodeOptionalFloat(dto.TopP, "messages request top_p is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	stopSequences, err := decodeStopSequenceArray(dto.StopSequences, "messages request stop_sequences is invalid")
	if err != nil {
		return canonical.GenerationControls{}, err
	}
	var effort *canonical.InferenceEffort
	if dto.OutputConfig != nil && strings.TrimSpace(dto.OutputConfig.Effort) != "" { // swobu:io-string source=boundary
		value := canonical.InferenceEffort(strings.TrimSpace(dto.OutputConfig.Effort)) // swobu:io-string source=boundary
		effort = &value
	}
	return canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: maxTokens,
		StopSequences:   stopSequences,
		Temperature:     temperature,
		TopP:            topP,
		Effort:          effort,
	})
}

func encodeMessagesGenerationControls(payload map[string]any, controls canonical.GenerationControls, reasoning canonical.ReasoningControls) error {
	maxTokens := defaultMessagesMaxTokens
	value, explicitMax := controls.Limits.MaxOutputTokens.Value()
	if explicitMax {
		maxTokens = value
	}
	if compute, ok := reasoning.ComputeField().Get(); ok && compute.Kind() == canonical.ReasoningBudget {
		budget, _ := compute.Tokens()
		if explicitMax && maxTokens <= budget {
			return canonical.BadRequest("messages max_tokens must be greater than reasoning budget_tokens")
		}
		if !explicitMax {
			if budget > int(^uint(0)>>1)-defaultMessagesMaxTokens {
				return canonical.BadRequest("messages reasoning budget is too large")
			}
			maxTokens = budget + defaultMessagesMaxTokens
		}
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
	if effort, ok := controls.Effort.Get(); ok {
		payload["output_config"] = map[string]any{"effort": effort}
	}
	return nil
}

// swobu:lint ignore string-switch because=Messages protocol boundary decodes thinking type and display variants.
func decodeMessagesReasoning(dto *messagesThinkingDTO) (canonical.ReasoningControls, error) {
	if dto == nil {
		return canonical.ReasoningControls{}, nil
	}
	params := canonical.ReasoningControlsParams{}
	switch strings.TrimSpace(dto.Type) { // swobu:io-string source=boundary
	case "disabled":
		params.Compute = canonical.Specify(canonical.NewDisabledReasoningCompute())
	case "adaptive":
		params.Compute = canonical.Specify(canonical.NewAutomaticReasoningCompute())
	case "enabled":
		compute, err := canonical.NewBudgetReasoningCompute(dto.BudgetTokens)
		if err != nil {
			return canonical.ReasoningControls{}, err
		}
		params.Compute = canonical.Specify(compute)
	default:
		return canonical.ReasoningControls{}, canonical.BadRequest("messages thinking type is invalid")
	}
	switch strings.TrimSpace(dto.Display) { // swobu:io-string source=boundary
	case "":
	case "summarized":
		params.Disclosure = canonical.Specify(canonical.ReasoningDisclosureSummary)
	case "omitted":
		params.Disclosure = canonical.Specify(canonical.ReasoningDisclosureNone)
	default:
		return canonical.ReasoningControls{}, canonical.BadRequest("messages thinking display is invalid")
	}
	return canonical.NewReasoningControls(params)
}

func encodeMessagesReasoning(payload map[string]any, reasoning canonical.ReasoningControls, omitAdaptiveThinking bool, changeLog *[]compat.Change) error {
	compute, computeSet := reasoning.ComputeField().Get()
	if computeSet {
		if disclosure, present := reasoning.DisclosureField().Get(); present && compute.Kind() == canonical.ReasoningDisabled && disclosure != canonical.ReasoningDisclosureNone {
			return provider.IncompatibleCapability(canonical.RequestReasoning, canonical.Occurrence{}, "Messages cannot represent disabled reasoning with readable disclosure")
		}
		if omitAdaptiveThinking && (compute.Kind() == canonical.ReasoningAutomatic || compute.Kind() == canonical.ReasoningBudget) {
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewApproximation(canonical.RequestReasoning, canonical.RequestReasoning, canonical.Occurrence{}))
			}
			return nil
		}
		thinking := map[string]any{}
		switch compute.Kind() {
		case canonical.ReasoningDisabled:
			thinking["type"] = "disabled"
		case canonical.ReasoningAutomatic:
			thinking["type"] = "adaptive"
		case canonical.ReasoningBudget:
			budget, _ := compute.Tokens()
			thinking["type"] = "enabled"
			thinking["budget_tokens"] = budget
		}
		if disclosure, ok := reasoning.DisclosureField().Get(); ok {
			if disclosure == canonical.ReasoningDisclosureNone {
				thinking["display"] = "omitted"
			} else if disclosure == canonical.ReasoningDisclosureSummary {
				thinking["display"] = "summarized"
			}
		}
		payload["thinking"] = thinking
	}
	return nil
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
