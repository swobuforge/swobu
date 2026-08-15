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
		{name: "custom is the catalog identity", provider: routing.Provider("custom"), protocol: "messages", want: true},
		{name: "native provider", provider: routing.Provider("openai"), protocol: "responses", want: true},
		{name: "unknown fails closed", provider: routing.Provider("openai"), protocol: "not_a_protocol", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := facts.ProtocolSupported(test.provider, test.protocol); got != test.want {
				t.Fatalf("ProtocolSupported(%q, %q) = %t, want %t", test.provider, test.protocol, got, test.want)
			}
		})
	}
	if facts.ProviderSupported == nil || facts.ConnectionShape == nil || facts.ValidateStandardConnection == nil || facts.BedrockRegionSupported == nil {
		t.Fatal("routing facts omitted a catalog predicate")
	}
}
