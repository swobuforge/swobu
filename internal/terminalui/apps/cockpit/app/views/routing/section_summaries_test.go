package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestEffectiveDraftBaseURL_BedrockDerivesFromRegion(t *testing.T) {
	t.Parallel()

	got := effectiveDraftBaseURL(state.ProviderConfigSnapshot{
		ProviderSpec: "bedrock",
		Region:       "eu-west-1",
	})
	want := "https://bedrock-runtime.eu-west-1.amazonaws.com/openai/v1"
	if got != want {
		t.Fatalf("base URL=%q want %q", got, want)
	}
}

func TestEffectiveDraftBaseURL_BedrockDerivesFromEnvWhenRegionMissing(t *testing.T) {
	t.Setenv("AWS_REGION", "us-west-2")
	t.Setenv("AWS_DEFAULT_REGION", "")

	got := effectiveDraftBaseURL(state.ProviderConfigSnapshot{
		ProviderSpec: "bedrock",
	})
	want := "https://bedrock-runtime.us-west-2.amazonaws.com/openai/v1"
	if got != want {
		t.Fatalf("base URL=%q want %q", got, want)
	}
}
