package telemetry

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

const providerFamilyOther = "other"

func normalizeProviderFamily(rawRoute string) string {
	route := strings.TrimSpace(strings.ToLower(rawRoute)) // trimlowerlint:allow boundary canonicalization
	if route == "" {
		return providerFamilyOther
	}
	spec := route
	if idx := strings.Index(spec, ":"); idx >= 0 {
		spec = strings.TrimSpace(spec[:idx]) // trimlowerlint:allow boundary canonicalization
	}
	if providercatalog.SupportsSpec(spec) {
		return spec
	}
	return providerFamilyOther
}
