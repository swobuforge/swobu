package providercatalog

import "strings"

// ProviderID is the canonical provider identity used across runtime seams.
type ProviderID string

// ParseProviderID parses one provider identifier from external input.
// Parsing is strict: callers must pass canonical values already validated at ingress.
func ParseProviderID(raw string) (ProviderID, bool) {
	normalized := strings.TrimSpace(raw) // swobu:io-string source=domain
	profile, ok := profileFor(normalized)
	if !ok {
		return "", false
	}
	return profile.ProviderID, true
}
