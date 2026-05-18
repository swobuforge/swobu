package requestpath

import "strings"

// CanonicalModelAlias returns one deterministic model identifier derived from
// the selected provider family and backend model id.
func CanonicalModelAlias(providerSpec, backendModelID string) string {
	providerSpec = strings.TrimSpace(providerSpec)     // swobu:io-string source=boundary
	backendModelID = strings.TrimSpace(backendModelID) // swobu:io-string source=boundary
	if providerSpec == "" {
		panic("requestpath.CanonicalModelAlias: providerSpec is required")
	}
	if backendModelID == "" {
		panic("requestpath.CanonicalModelAlias: backendModelID is required")
	}
	return providerSpec + ":" + backendModelID
}
