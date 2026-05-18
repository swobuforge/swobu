package model

import "strings"

const authEndpointProviderLocatorDelimiter = "#"
const authSubjectLocatorPrefix = "subject:"

// EncodeAuthEndpointProviderLocator encodes the endpoint and provider config
// locator used by daemon auth session persistence.
func EncodeAuthEndpointProviderLocator(endpointName string, providerRef string) string {
	return strings.TrimSpace(endpointName) + authEndpointProviderLocatorDelimiter + strings.TrimSpace(providerRef) // swobu:io-string source=boundary
}

// EncodeAuthTransientSubjectLocator encodes a pre-create auth subject locator
// used by cockpit add-model login before provider config persistence.
func EncodeAuthTransientSubjectLocator(endpointName string, draftRef string) string {
	return authSubjectLocatorPrefix + strings.TrimSpace(endpointName) + authEndpointProviderLocatorDelimiter + strings.TrimSpace(draftRef) // swobu:io-string source=boundary
}

// IsAuthTransientSubject reports whether auth subject targets a transient
// pre-persistence flow (for example add-model draft login).
func IsAuthTransientSubject(subject string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), authSubjectLocatorPrefix) // swobu:io-string source=boundary
}
