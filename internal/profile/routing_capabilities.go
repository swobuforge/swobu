package profile

import "github.com/swobuforge/swobu/internal/routing"

// RoutingCapabilities adapts the provider catalog to routing's construction
// contract. Construction boundaries use this function so provider aliases and
// conservative protocol defaults cannot diverge by transport.
func RoutingCapabilities() routing.TargetCapabilities {
	return routing.TargetCapabilities{
		ProtocolSupported: func(provider routing.Provider, protocol string) bool {
			spec := string(provider)
			if provider == routing.ProviderCustom {
				spec = "openai_compatible"
			}
			return protocol != ProviderProtocolAuto && SupportsProviderProtocolForSpec(spec, protocol)
		},
		NormalizeAzureProjectEndpoint: NormalizeAzureProjectEndpoint,
		BedrockRegionSupported:        SupportsBedrockMantleRegion,
	}
}
