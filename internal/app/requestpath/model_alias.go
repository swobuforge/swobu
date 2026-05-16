package requestpath

import "strings"

// CanonicalModelAlias returns one deterministic model identifier derived from
// the selected provider family and backend model id.
func CanonicalModelAlias(providerSpec, backendModelID string) string {
	providerSpec = strings.TrimSpace(providerSpec)     // trimlowerlint:allow boundary canonicalization
	backendModelID = strings.TrimSpace(backendModelID) // trimlowerlint:allow boundary canonicalization
	if providerSpec == "" {
		panic("requestpath.CanonicalModelAlias: providerSpec is required")
	}
	if backendModelID == "" {
		panic("requestpath.CanonicalModelAlias: backendModelID is required")
	}
	return providerSpec + ":" + backendModelID
}
