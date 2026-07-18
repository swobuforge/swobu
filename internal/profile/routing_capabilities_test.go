package profile

import (
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestRoutingCapabilitiesOwnsCatalogProtocolMapping(t *testing.T) {
	capabilities := RoutingCapabilities()
	tests := []struct {
		name     string
		provider routing.Provider
		protocol string
		want     bool
	}{
		{name: "custom maps to catalog spec", provider: routing.ProviderCustom, protocol: "messages", want: true},
		{name: "native provider", provider: routing.ProviderOpenAI, protocol: "responses", want: true},
		{name: "auto fails closed", provider: routing.ProviderOpenAI, protocol: ProviderProtocolAuto, want: false},
		{name: "unknown fails closed", provider: routing.ProviderOpenAI, protocol: "unknown", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := capabilities.ProtocolSupported(test.provider, test.protocol); got != test.want {
				t.Fatalf("ProtocolSupported(%q, %q) = %t, want %t", test.provider, test.protocol, got, test.want)
			}
		})
	}
	if capabilities.NormalizeAzureProjectEndpoint == nil || capabilities.BedrockRegionSupported == nil {
		t.Fatal("routing capabilities omitted a catalog predicate")
	}
}
