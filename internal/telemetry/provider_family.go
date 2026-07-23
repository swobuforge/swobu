package telemetry

import (
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
)

const providerFamilyOther = "other"

// NormalizeProviderFamily collapses a provider route/endpoint to a bounded
// provider-family enum. Custom or unrecognized routes resolve to "other" so no
// user-specific endpoint (re-identifying) ever leaves the machine.
func NormalizeProviderFamily(rawRoute string) string {
	route := strings.TrimSpace(strings.ToLower(rawRoute)) // swobu:io-string source=boundary
	if route == "" {
		return providerFamilyOther
	}
	spec := route
	if idx := strings.Index(spec, ":"); idx >= 0 {
		spec = strings.TrimSpace(spec[:idx]) // swobu:io-string source=boundary
	}
	if profile.SupportsSpec(spec) {
		return spec
	}
	return providerFamilyOther
}
