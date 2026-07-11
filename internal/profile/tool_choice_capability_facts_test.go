package profile

import (
	"strings"
	"testing"

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
	// retry across OpenAI-style adapters.
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

	// Real-world-derived chat model facts from scenario capture captures:
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

func TestToolChoiceCapabilityFacts_IncludeResponsesDefaultsForResponsesProviders(t *testing.T) {
	facts := ToolChoiceCapabilityFacts()

	for _, provider := range []string{"openai", "openrouter", "openai_compatible"} {
		found := false
		for _, fact := range facts {
			if fact.ProviderSpec != provider || fact.ProtocolKind != "responses" || fact.ModelID != "*" {
				continue
			}
			found = true
			if !fact.ImmediateDowngradeRetry {
				t.Fatalf("provider %q responses wildcard fact has ImmediateDowngradeRetry=false, want true", provider)
			}
		}
		if !found {
			t.Fatalf("missing responses wildcard tool-choice fact for provider %q", provider)
		}
	}
}

func TestToolChoiceCapabilityFacts_IncludeProviderModelOverrides(t *testing.T) {
	facts := ToolChoiceCapabilityFacts()

	wantModels := map[string]bool{
		"nvidia/nemotron-3-super-120b-a12b":   false,
		"arcee-ai/trinity-large-preview:free": false,
	}
	for _, fact := range facts {
		if fact.ProviderSpec != "openrouter" || fact.ProtocolKind != "chat_completions" {
			continue
		}
		if _, ok := wantModels[fact.ModelID]; !ok {
			continue
		}
		wantModels[fact.ModelID] = true
		if !fact.ImmediateDowngradeRetry {
			t.Fatalf("model %q ImmediateDowngradeRetry=false, want true", fact.ModelID)
		}
	}
	for model, seen := range wantModels {
		if !seen {
			t.Fatalf("missing openrouter model-specific tool-choice fact for model %q", model)
		}
	}
}
