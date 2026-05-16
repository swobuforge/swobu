package requestpath

import (
	"strings"
)

const (
	PublicModelIDSwobu     = "swobu"
	modelResolutionRuntime = "runtime"
	modelResolutionClient  = "client_swobu"
	modelResolutionIgnored = "client_ignored"
)

func validateRequestedPublicModel(raw string) string {
	requested := strings.TrimSpace(raw) // trimlowerlint:allow boundary canonicalization
	if requested == "" {
		return modelResolutionRuntime
	}
	if strings.EqualFold(requested, PublicModelIDSwobu) { // trimlowerlint:allow boundary canonicalization
		return modelResolutionClient
	}
	// Compatibility ingress may still send backend model literals; Swobu runtime
	// remains the source of truth and ignores those selectors.
	return modelResolutionIgnored
}
