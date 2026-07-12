package profile

import (
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

func RequiresCredential(spec, baseURL string) bool {
	return requiresCredentialFromModes(AllowedAuthModesForSpec(spec), baseURL)
}

// RequiresExplicitExecuteBaseURL reports whether the operator must set an
// explicit non-default execute base URL for this provider spec.
func RequiresExplicitExecuteBaseURL(spec string) bool {
	normalized := strings.TrimSpace(spec) // swobu:io-string source=boundary
	if normalized == string(ProviderSpecOpenAICompatible) || normalized == string(ProviderSpecAzure) {
		return true
	}
	return false
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
			// Explicit always requirement: keep default credential requirement path.
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
