package providercatalog

import "github.com/metrofun/swobu/internal/domain/protocolsurface"

// ToolChoiceCapabilityFact declares one provider/model/protocol capability fact
// for tool-choice policy behavior.
//
// ModelID accepts "*" for provider+protocol defaults and concrete model IDs for
// model-specific overrides.
// Keep this schema minimal: only include fields consumed by active request-path
// policy behavior. Add new fields only with a concrete downstream consumer.
type ToolChoiceCapabilityFact struct {
	ProviderSpec            string
	ProtocolKind            protocolsurface.Kind
	ModelID                 string
	ImmediateDowngradeRetry bool
}

// ToolChoiceCapabilityFacts returns default and model-specific tool-choice
// behavior facts used by request-path semantic policy.
func ToolChoiceCapabilityFacts() []ToolChoiceCapabilityFact {
	out := make([]ToolChoiceCapabilityFact, 0, len(catalog())+4)

	// Baseline: responses protocol supports strict->auto immediate downgrade
	// retry across provider specs that expose responses protocol in catalog.
	for _, profile := range catalog() {
		if !supportsProtocol(profile.SupportedProtocols, protocolsurface.Responses) {
			continue
		}
		out = append(out, ToolChoiceCapabilityFact{
			ProviderSpec:            profile.Spec,
			ProtocolKind:            protocolsurface.Responses,
			ModelID:                 "*",
			ImmediateDowngradeRetry: true,
		})
	}

	// Real-world-derived chat model facts from live matrix captures:
	// some OpenRouter-routed models reject strict tool_choice while still
	// supporting auto tool_choice behavior.
	out = append(out,
		ToolChoiceCapabilityFact{
			ProviderSpec:            "openrouter",
			ProtocolKind:            protocolsurface.ChatCompletions,
			ModelID:                 "nvidia/nemotron-3-super-120b-a12b",
			ImmediateDowngradeRetry: true,
		},
		ToolChoiceCapabilityFact{
			ProviderSpec:            "openrouter",
			ProtocolKind:            protocolsurface.ChatCompletions,
			ModelID:                 "arcee-ai/trinity-large-preview:free",
			ImmediateDowngradeRetry: true,
		},
	)

	return out
}

func supportsProtocol(kinds []protocolsurface.Kind, target protocolsurface.Kind) bool {
	for _, kind := range kinds {
		if kind == target {
			return true
		}
	}
	return false
}
