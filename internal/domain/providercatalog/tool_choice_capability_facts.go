package providercatalog

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ToolChoiceCapabilityFact declares one provider/model/protocol capability fact
// for tool-choice policy behavior.
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

// ToolChoiceCapabilityFacts returns default and model-specific tool-choice
// behavior facts used by request-path semantic policy.
func ToolChoiceCapabilityFacts() []ToolChoiceCapabilityFact {
	out := make([]ToolChoiceCapabilityFact, 0, len(catalog())+4)

	// Baseline: responses protocol supports strict->auto immediate downgrade
	// retry across OpenAI-compatible adapters.
	for _, profile := range catalog() {
		if strings.EqualFold(string(profile.ProviderID), "anthropic") {
			continue
		}
		out = append(out, ToolChoiceCapabilityFact{
			ProviderSpec:            string(profile.ProviderID),
			ProtocolKind:            protocolkind.Responses,
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
