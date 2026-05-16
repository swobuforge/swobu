package routing

import (
	"regexp"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestProviderHumanIdentifier_PrefersAlias(t *testing.T) {
	t.Parallel()

	pc := state.ProviderConfigSnapshot{
		ProviderSpec:  "openai",
		ModelID:       "gpt-5",
		CredentialRef: "env:OPENAI_API_KEY",
		TargetAlias:   "fast",
	}
	if got := providerHumanIdentifier(pc); got != "fast" {
		t.Fatalf("providerHumanIdentifier(alias)=%q want %q", got, "fast")
	}
}

func TestProviderHumanIdentifier_FallbackFormat(t *testing.T) {
	t.Parallel()

	pc := state.ProviderConfigSnapshot{
		ProviderSpec:  "chatgpt",
		ModelID:       "gpt-5.3-codex",
		CredentialRef: "keychain:chatgpt/default",
	}
	got := providerHumanIdentifier(pc)
	if !regexp.MustCompile(`^chatgpt:gpt-5\.3-codex:[0-9a-f]{8}$`).MatchString(got) {
		t.Fatalf("providerHumanIdentifier(fallback)=%q", got)
	}
}

func TestProviderHumanIdentifier_IncompleteConfigReturnsNotConfigured(t *testing.T) {
	t.Parallel()

	pc := state.ProviderConfigSnapshot{
		ProviderSpec: "openai",
		ModelID:      "",
	}
	if got := providerHumanIdentifier(pc); got != unresolvedProviderIdentifier {
		t.Fatalf("providerHumanIdentifier(incomplete)=%q", got)
	}
}

func TestProviderDisplayLabel_UsesCanonicalProviderIdentifier(t *testing.T) {
	t.Parallel()

	pc := state.ProviderConfigSnapshot{
		ProviderSpec:  "chatgpt",
		ModelID:       "gpt-5.4-mini",
		CredentialRef: "keychain:chatgpt/default",
	}
	if got, want := providerDisplayLabel(pc), providerHumanIdentifier(pc); got != want {
		t.Fatalf("providerDisplayLabel()=%q want canonical %q", got, want)
	}
}
