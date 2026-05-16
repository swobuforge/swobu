package providercatalog

import "testing"

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
