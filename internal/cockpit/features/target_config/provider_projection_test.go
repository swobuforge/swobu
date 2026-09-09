package target_config

import (
	"slices"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/testkit/cockpittestkit"
)

func TestOpenAIProviderPickerKeywordsExpressConnectionChoice(t *testing.T) {
	for _, test := range []struct {
		option readmodel.ProviderOptionReadModel
		want   []string
	}{
		{option: readmodel.ProviderOptionReadModel{ProviderSpec: "openai", DisplayName: "OpenAI API", SetupHint: "API key"}, want: []string{"openai", "API key", "OpenAI, API, API key, Codex"}},
		{option: readmodel.ProviderOptionReadModel{ProviderSpec: "chatgpt", DisplayName: "OpenAI · ChatGPT subscription", SetupHint: "browser login"}, want: []string{"chatgpt", "browser login", "OpenAI, ChatGPT, Codex, subscription, OAuth, sign in, browser login, device code"}},
	} {
		if got := providerPickerKeywords(test.option); !slices.Equal(got, test.want) {
			t.Fatalf("keywords for %q = %#v, want %#v", test.option.ProviderSpec, got, test.want)
		}
	}
}

func TestProviderPickerCodexSearchShowsAdjacentOpenAIChoices(t *testing.T) {
	config := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	config.UpdateProviderOptions([]readmodel.ProviderOptionReadModel{
		{ProviderSpec: "openai", DisplayName: "OpenAI API", SetupHint: "API key"},
		{ProviderSpec: "chatgpt", DisplayName: "OpenAI · ChatGPT subscription", SetupHint: "browser login"},
		{ProviderSpec: "anthropic", DisplayName: "Anthropic", SetupHint: "API key"},
	})
	config.Open()
	harness, err := testkit.NewHarness(config)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer harness.Close()
	harness.Open()
	for _, char := range "codex" {
		harness.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: char})
	}
	frame := harness.FrameTrimmed()
	apiIndex := strings.Index(frame, "OpenAI API")
	subscriptionIndex := strings.Index(frame, "OpenAI · ChatGPT subscription")
	if apiIndex < 0 || subscriptionIndex <= apiIndex || strings.Contains(frame, "Anthropic") {
		t.Fatalf("codex search must show only adjacent OpenAI choices:\n%s", frame)
	}
	if !strings.Contains(frame, "2 of 3 shown") {
		t.Fatalf("codex search count is not explicit:\n%s", frame)
	}
}

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
