package profile

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestToolChoiceCapabilityFacts_IncludeResponsesDefaultsForResponsesProviders(t *testing.T) {
	facts := ToolChoiceCapabilityFacts()
	if len(facts) == 0 {
		t.Fatal("tool-choice capability facts must not be empty")
	}

	expected := map[string]bool{}
	for _, entry := range All() {
		if !SupportsExecutionProtocolForSpec(string(entry.ProviderID), protocolkind.Responses) {
			continue
		}
		expected[string(entry.ProviderID)] = false
	}

	for _, fact := range facts {
		if fact.ProtocolKind != protocolkind.Responses || fact.ModelID != "*" {
			continue
		}
		if _, ok := expected[fact.ProviderSpec]; !ok {
			t.Fatalf("unexpected responses wildcard tool-choice fact for provider %q", fact.ProviderSpec)
		}
		if expected[fact.ProviderSpec] {
			t.Fatalf("duplicate responses wildcard tool-choice fact for provider %q", fact.ProviderSpec)
		}
		expected[fact.ProviderSpec] = true
		if !fact.ImmediateDowngradeRetry {
			t.Fatalf("provider %q responses wildcard fact has ImmediateDowngradeRetry=false, want true", fact.ProviderSpec)
		}
	}

	for provider, seen := range expected {
		if !seen {
			t.Fatalf("missing responses wildcard tool-choice fact for provider %q", provider)
		}
	}
	if !SupportsToolChoiceImmediateDowngradeRetry(ProviderSpecBedrock, protocolkind.Responses, "any") {
		t.Fatal("bedrock responses should inherit tool-choice retry facts")
	}
}

func TestToolChoiceCapabilityFacts_IncludeProviderModelOverrides(t *testing.T) {
	facts := ToolChoiceCapabilityFacts()

	wantModels := map[string]bool{
		"nvidia/nemotron-3-super-120b-a12b":   false,
		"arcee-ai/trinity-large-preview:free": false,
	}
	for _, fact := range facts {
		if fact.ProviderSpec != "openrouter" || fact.ProtocolKind != protocolkind.ChatCompletions {
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

func TestSupportsToolChoiceImmediateDowngradeRetry_ConservativeFacts(t *testing.T) {
	if !SupportsToolChoiceImmediateDowngradeRetry(ProviderSpecOpenAI, protocolkind.Responses, "gpt-4.1-mini") {
		t.Fatal("openai responses should support immediate tool-choice downgrade retry")
	}
	if !SupportsToolChoiceImmediateDowngradeRetry(ProviderSpecOpenRouter, protocolkind.ChatCompletions, "nvidia/nemotron-3-super-120b-a12b") {
		t.Fatal("openrouter model override should support immediate tool-choice downgrade retry")
	}
	if SupportsToolChoiceImmediateDowngradeRetry(ProviderSpecOpenRouter, protocolkind.ChatCompletions, "unknown/model") {
		t.Fatal("unknown openrouter model should not inherit immediate tool-choice downgrade retry")
	}
	if !SupportsToolChoiceImmediateDowngradeRetry(ProviderSpecBedrock, protocolkind.Responses, "any") {
		t.Fatal("bedrock responses should support immediate tool-choice downgrade retry")
	}
}
