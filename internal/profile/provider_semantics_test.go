package profile

import (
	"slices"
	"testing"
)

func TestNousIsNativeChatOnly(t *testing.T) {
	want := []string{"chat_completions", "chat_completions_stream"}
	if got := ConcreteProviderProtocolsForSpec(string(ProviderSpecNous)); !slices.Equal(got, want) {
		t.Fatalf("Nous protocols = %v, want %v", got, want)
	}
}

func TestNewProviderFixedLocators(t *testing.T) {
	for providerID, want := range map[ProviderID]string{
		ProviderSpecCompactifAI: "https://api.compactif.ai/v1",
		ProviderSpecOpenCodeZen: "https://opencode.ai/zen/v1",
		ProviderSpecNous:        "https://inference-api.nousresearch.com/v1",
		ProviderSpecCommandCode: "https://api.commandcode.ai/provider/v1",
		ProviderSpecVenice:      "https://api.venice.ai/api/v1",
	} {
		provider, ok := ProfileForSpec(string(providerID))
		if !ok || provider.Locator.Default != want {
			t.Fatalf("%s locator = %q, want %q", providerID, provider.Locator.Default, want)
		}
	}
}
