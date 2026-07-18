package profile

import (
	"net"
	"net/url"
	"slices"
	"strings"
)

// DefaultAuthHeaderForSpec returns the canonical auth header name for a
// provider spec, or empty if the provider does not expose a header choice.
func DefaultAuthHeaderForSpec(spec string) string {
	profile, ok := profileFor(spec)
	if !ok {
		return ""
	}
	return profile.DefaultAuthHeader
}

// SupportedAuthHeadersForSpec returns the common auth-header picker options for
// a provider spec. Manual entry remains a separate escape hatch.
func SupportedAuthHeadersForSpec(spec string) []string {
	normalized := strings.TrimSpace(spec) // swobu:io-string source=boundary
	if normalized == string(ProviderSpecCustom) {
		return slices.Clone(customAuthHeaders)
	}
	return nil
}

// InferredCredentialHeaderForBackendURL returns the initial default credential
// header inferred from a Custom Endpoint backend URL. It is a cheap heuristic
// seed only: the operator can always change it, and no credential is ever
// selected implicitly. Unknown or unparsable URLs fall back to the profile
// default (Authorization).
//
// Rules (RFC: Custom Endpoint Credential Header):
//   - path contains /anthropic/            -> x-api-key
//   - Azure Foundry Anthropic-looking host -> x-api-key
//   - Azure OpenAI-looking host            -> api-key
//   - otherwise                            -> Authorization
func InferredCredentialHeaderForBackendURL(baseURL string) string {
	fallback := DefaultAuthHeaderForSpec(string(ProviderSpecCustom))
	raw := strings.TrimSpace(baseURL) // swobu:io-string source=boundary
	if raw == "" {
		return fallback
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fallback
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.ToLower(parsed.Path)
	switch {
	case strings.Contains(path, "/anthropic/"):
		return "x-api-key"
	case isAzureFoundryHost(host) && strings.Contains(path, "anthropic"):
		return "x-api-key"
	case isAzureOpenAIHost(host):
		return "api-key"
	default:
		return fallback
	}
}

func isAzureFoundryHost(host string) bool {
	return strings.HasSuffix(host, ".services.ai.azure.com") ||
		strings.HasSuffix(host, ".ai.azure.com")
}

func isAzureOpenAIHost(host string) bool {
	return strings.HasSuffix(host, ".openai.azure.com") ||
		strings.HasSuffix(host, ".cognitiveservices.azure.com")
}

func RequiresCredential(spec, baseURL string) bool {
	provider, ok := profileFor(spec)
	if !ok {
		return false
	}
	switch provider.Credential.Requirement {
	case CredentialUnsupported, CredentialOptional:
		return false
	case CredentialRequiredOutsideLoopback:
		return !isLoopbackHTTP(baseURL)
	case CredentialRequired:
		return true
	default:
		// Unknown policy fails closed.
		return true
	}
}

func isLoopbackHTTP(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
