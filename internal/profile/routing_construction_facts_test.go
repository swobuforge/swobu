package profile

import (
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestRoutingConstructionFactsUsesDirectProviderIdentity(t *testing.T) {
	facts := RoutingConstructionFacts()
	tests := []struct {
		name     string
		provider routing.Provider
		protocol string
		want     bool
	}{
		{name: "custom is the catalog identity", provider: routing.ProviderCustom, protocol: "messages", want: true},
		{name: "native provider", provider: routing.ProviderOpenAI, protocol: "responses", want: true},
		{name: "auto fails closed", provider: routing.ProviderOpenAI, protocol: ProviderProtocolAuto, want: false},
		{name: "unknown fails closed", provider: routing.ProviderOpenAI, protocol: "unknown", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := facts.ProtocolSupported(test.provider, test.protocol); got != test.want {
				t.Fatalf("ProtocolSupported(%q, %q) = %t, want %t", test.provider, test.protocol, got, test.want)
			}
		})
	}
	if facts.NormalizeAzureProjectEndpoint == nil || facts.BedrockRegionSupported == nil {
		t.Fatal("routing facts omitted a catalog predicate")
	}
}
