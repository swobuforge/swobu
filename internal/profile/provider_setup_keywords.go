package profile

import (
	"strings"

	"slices"
)

// ProviderSetupKeywordsForSpec returns non-authoritative search/copy keywords
// declared for one provider spec.
func ProviderSetupKeywordsForSpec(spec string) []string {
	provider, ok := profileFor(spec)
	if !ok || len(provider.SetupKeywords) == 0 {
		return nil
	}
	return slices.Clone(provider.SetupKeywords)
}

// ProviderSetupKeywordSummaryForSpec returns a compact picker-search inventory.
// It must not be used as setup behavior authority.
func ProviderSetupKeywordSummaryForSpec(spec string) string {
	keywords := ProviderSetupKeywordsForSpec(spec)
	if len(keywords) == 0 {
		return ""
	}
	return strings.Join(keywords, ", ")
}

func EndpointLabelForProvider(spec string) string {
	endpoint, ok := EndpointSpecForProvider(spec)
	if !ok {
		return ""
	}
	return strings.TrimSpace(endpoint.Label)
}
