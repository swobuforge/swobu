package target_config

import (
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestCurrentTargetDraftPersistsOnlyAuthorableLocator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider profile.ProviderID
		locator  string
		want     string
	}{
		{
			name:     "fixed DeepSeek operational base URL remains non-durable",
			provider: profile.ProviderSpecDeepSeek,
			locator:  "https://api.deepseek.com/anthropic/v1",
			want:     "",
		},
		{
			name:     "fixed Kimi operational base URL remains non-durable",
			provider: profile.ProviderSpecKimi,
			locator:  "https://api.moonshot.ai/v1",
			want:     "",
		},
		{
			name:     "custom base URL remains durable",
			provider: profile.ProviderSpecCustom,
			locator:  "https://provider.example/v1",
			want:     "https://provider.example/v1",
		},
		{
			name:     "Bedrock owns locator outside generic buffer",
			provider: profile.ProviderSpecBedrock,
			locator:  "https://bedrock-mantle.eu-west-2.api.aws/v1",
			want:     "retained only when provider owns an authorable locator",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			draft := currentTargetDraft(readmodel.TargetDraft{
				ProviderSpec: string(test.provider),
				Locator:      "retained only when provider owns an authorable locator",
			}, test.locator, "model", "responses", "chat")
			if draft.Locator != test.want {
				t.Fatalf("locator = %q, want %q", draft.Locator, test.want)
			}
		})
	}
}
