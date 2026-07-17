package profile

import (
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
	if normalized == string(ProviderSpecOpenAICompatible) {
		return slices.Clone(openAICompatibleAuthHeaders)
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
	fallback := DefaultAuthHeaderForSpec(string(ProviderSpecOpenAICompatible))
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
	return requiresCredentialFromModes(AllowedAuthModesForSpec(spec), baseURL)
}

func requiresCredentialFromModes(modes []AuthModeSpec, baseURL string) bool {
	if len(modes) == 0 {
		return false
	}
	normalizedBaseURL := baseURL
	hasNeverMode := false
	hasLoopbackConditional := false
	for _, mode := range modes {
		switch mode.Requirement {
		case AuthModeRequirementNever:
			hasNeverMode = true
		case AuthModeRequirementAlways:
			// Explicit always requirement: keep credential-required path.
		case AuthModeRequirementExceptLoopbackExecute:
			hasLoopbackConditional = true
		default:
			// Unknown requirements fall back to requiring credentials.
		}
	}
	if hasNeverMode {
		return false
	}
	if hasLoopbackConditional {
		return !(strings.HasPrefix(normalizedBaseURL, "http://127.0.0.1") || strings.HasPrefix(normalizedBaseURL, "http://localhost"))
	}
	return true
}

func InferAuthKind(spec, baseURL, credentialRef string) AuthKind {
	if strings.TrimSpace(credentialRef) != "" { // swobu:io-string source=domain
		return AuthCredentialRef
	}
	if RequiresCredential(spec, baseURL) {
		return AuthCredentialRef
	}
	return AuthNone
}

type RouteProfile struct {
	ProviderSpec string
	AuthKind     AuthKind
}

// ResolveRouteProfile resolves one execution-route profile from durable target
// intent.
func ResolveRouteProfile(spec string, baseURL, credentialRef string) (RouteProfile, bool) {
	if !SupportsSpec(spec) {
		return RouteProfile{}, false
	}
	authKind := InferAuthKind(spec, baseURL, credentialRef)
	if !SupportsAuth(spec, authKind) {
		return RouteProfile{}, false
	}
	return RouteProfile{
		ProviderSpec: spec,
		AuthKind:     authKind,
	}, true
}
