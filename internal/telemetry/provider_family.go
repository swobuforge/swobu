package telemetry

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

const providerFamilyOther = "other"

func normalizeProviderFamily(rawRoute string) string {
	route := strings.TrimSpace(strings.ToLower(rawRoute)) // swobu:io-string source=boundary
	if route == "" {
		return providerFamilyOther
	}
	spec := route
	if idx := strings.Index(spec, ":"); idx >= 0 {
		spec = strings.TrimSpace(spec[:idx]) // swobu:io-string source=boundary
	}
	if providercatalog.SupportsSpec(spec) {
		return spec
	}
	return providerFamilyOther
}
