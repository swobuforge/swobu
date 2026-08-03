package profile

import "github.com/swobuforge/swobu/internal/routing"

// RoutingConstructionFacts adapts the provider catalog to routing's construction
// contract. Construction boundaries use this function so provider aliases and
// conservative protocol defaults cannot diverge by transport.
func RoutingConstructionFacts() routing.TargetConstructionFacts {
	return routing.TargetConstructionFacts{
		ProtocolSupported: func(provider routing.Provider, protocol string) bool {
			return protocol != ProviderProtocolAuto && SupportsProviderProtocolForSpec(string(provider), protocol)
		},
		DerivedProtocol: func(provider routing.Provider) (string, bool) {
			return DerivedProtocolForSpec(string(provider))
		},
		NormalizeAzureProjectEndpoint: NormalizeAzureProjectEndpoint,
		BedrockRegionSupported:        SupportsBedrockMantleRegion,
	}
}
