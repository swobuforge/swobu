package profile

import "github.com/swobuforge/swobu/internal/domain/protocolkind"

// ToolChoiceCapabilityFact declares one provider/model/protocol capability fact
// for tool-choice retry behavior.
//
// ModelID accepts "*" for provider+protocol defaults and concrete model IDs for
// model-specific overrides.
// Keep this schema minimal: only include fields consumed by active request-path
// policy behavior. Add new fields only with a concrete downstream consumer.
type ToolChoiceCapabilityFact struct {
	ProviderSpec            string
	ProtocolKind            protocolkind.ProtocolKind
	ModelID                 string
	ImmediateDowngradeRetry bool
}

// ToolChoiceCapabilityFacts returns conservative default and model-specific
// tool-choice behavior facts used by request-path semantic policy.
//
// Only responses-capable routes are included in the wildcard baseline. Route
// support stays conservative: if a provider does not actually expose the
// responses protocol, it cannot inherit the responses tool-choice retry fact.
func ToolChoiceCapabilityFacts() []ToolChoiceCapabilityFact {
	out := make([]ToolChoiceCapabilityFact, 0, len(catalog())+4)

	for _, entry := range catalog() {
		if !SupportsExecutionProtocolForSpec(string(entry.ProviderID), protocolkind.Responses) {
			continue
		}
		out = append(out, ToolChoiceCapabilityFact{
			ProviderSpec:            string(entry.ProviderID),
			ProtocolKind:            protocolkind.Responses,
			ModelID:                 "*",
			ImmediateDowngradeRetry: true,
		})
	}

	// Real-world-derived chat model facts from scenario capture: some
	// OpenRouter-routed models reject strict tool_choice while still supporting
	// auto tool_choice behavior.
	out = append(out,
		ToolChoiceCapabilityFact{
			ProviderSpec:            "openrouter",
			ProtocolKind:            protocolkind.ChatCompletions,
			ModelID:                 "nvidia/nemotron-3-super-120b-a12b",
			ImmediateDowngradeRetry: true,
		},
		ToolChoiceCapabilityFact{
			ProviderSpec:            "openrouter",
			ProtocolKind:            protocolkind.ChatCompletions,
			ModelID:                 "arcee-ai/trinity-large-preview:free",
			ImmediateDowngradeRetry: true,
		},
	)

	return out
}

// SupportsToolChoiceImmediateDowngradeRetry reports whether tool-choice can
// immediately retry by downgrading strict selection to auto for this
// provider/protocol/model tuple. Unknown combinations fail closed.
func SupportsToolChoiceImmediateDowngradeRetry(provider ProviderID, protocol protocolkind.ProtocolKind, modelID string) bool {
	facts := ToolChoiceCapabilityFacts()
	for _, fact := range facts {
		if fact.ProviderSpec != string(provider) || fact.ProtocolKind != protocol || fact.ModelID != modelID {
			continue
		}
		return fact.ImmediateDowngradeRetry
	}
	for _, fact := range facts {
		if fact.ProviderSpec != string(provider) || fact.ProtocolKind != protocol || fact.ModelID != "*" {
			continue
		}
		return fact.ImmediateDowngradeRetry
	}
	return false
}
