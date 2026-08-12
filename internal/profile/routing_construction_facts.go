package profile

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/routing"
)

// RoutingConstructionFacts adapts the provider catalog to routing's construction
// contract. Construction boundaries use this function so provider aliases and
// conservative protocol defaults cannot diverge by transport.
func RoutingConstructionFacts() routing.TargetConstructionFacts {
	return routing.TargetConstructionFacts{
		ProviderSupported: SupportsSpec,
		ConnectionShape: func(provider routing.Provider) (routing.ConnectionShape, bool) {
			return ConnectionShapeForSpec(string(provider))
		},
		ValidateStandardConnection: ValidateStandardConnection,
		ProtocolSupported: func(provider routing.Provider, protocol string) bool {
			return protocol != ProviderProtocolAuto && SupportsProviderProtocolForSpec(string(provider), protocol)
		},
		DerivedProtocol: func(provider routing.Provider) (string, bool) {
			return DerivedProtocolForSpec(string(provider))
		},
		NormalizeAzureProjectLocator: NormalizeAzureProjectEndpoint,
		BedrockRegionSupported:       SupportsBedrockMantleRegion,
	}
}

// ValidateStandardConnection applies the complete profile-owned durable
// connection contract for one ordinary provider. Routing supplies this one
// construction fact and remains unaware of locator or credential policies.
func ValidateStandardConnection(provider routing.Provider, draft routing.StandardConnectionDraft) (routing.StandardConnectionDraft, error) {
	entry, ok := profileFor(string(provider))
	if !ok {
		return routing.StandardConnectionDraft{}, fmt.Errorf("connection.%s: provider is unsupported", provider)
	}
	return validateStandardConnectionForProfile(entry, provider, draft)
}

func validateStandardConnectionForProfile(entry Profile, provider routing.Provider, draft routing.StandardConnectionDraft) (routing.StandardConnectionDraft, error) {
	if entry.ConnectionShape != routing.ConnectionShapeStandard {
		return routing.StandardConnectionDraft{}, fmt.Errorf("connection.%s: standard connection is unsupported", provider)
	}

	draft.Locator = strings.TrimSpace(draft.Locator)
	draft.Credential = strings.TrimSpace(draft.Credential)
	effectiveLocator := draft.Locator
	field := standardLocatorField(entry)
	switch entry.Locator.Kind {
	case LocatorFixed:
		if draft.Locator != "" {
			return routing.StandardConnectionDraft{}, standardConnectionError(provider, field, "is not authorable")
		}
		effectiveLocator = entry.Locator.Default
	case LocatorBaseURL:
		if effectiveLocator == "" {
			effectiveLocator = strings.TrimSpace(entry.Locator.Default)
		}
		if effectiveLocator == "" {
			return routing.StandardConnectionDraft{}, standardConnectionError(provider, field, "is required")
		}
	case LocatorAzureProject:
		normalized, err := NormalizeAzureProjectEndpoint(draft.Locator)
		if err != nil {
			return routing.StandardConnectionDraft{}, standardConnectionError(provider, field, err.Error())
		}
		draft.Locator = normalized
		effectiveLocator = normalized
	default:
		return routing.StandardConnectionDraft{}, standardConnectionError(provider, field, "has an unsupported profile locator")
	}

	if entry.ProviderID == ProviderSpecLMStudio {
		if err := validateLMStudioExecutionBase(effectiveLocator); err != nil {
			return routing.StandardConnectionDraft{}, standardConnectionError(provider, field, err.Error())
		}
	}

	switch entry.Credential.Requirement {
	case CredentialRequired:
		if draft.Credential == "" {
			return routing.StandardConnectionDraft{}, standardConnectionError(provider, "credential", "is required")
		}
	case CredentialOptional:
		// Either a durable reference or provider-owned ambient authentication is valid.
	case CredentialUnsupported:
		if draft.Credential != "" {
			return routing.StandardConnectionDraft{}, standardConnectionError(provider, "credential", "is not authorable")
		}
	case CredentialRequiredOutsideLoopback:
		if RequiresCredential(string(provider), effectiveLocator) && draft.Credential == "" {
			return routing.StandardConnectionDraft{}, standardConnectionError(provider, "credential", "is required")
		}
	default:
		return routing.StandardConnectionDraft{}, standardConnectionError(provider, "credential", "has an unsupported profile requirement")
	}
	return draft, nil
}

func standardLocatorField(entry Profile) string {
	if entry.Locator.Kind == LocatorAzureProject {
		return "project_endpoint"
	}
	return "base_url"
}

func standardConnectionError(provider routing.Provider, field, message string) error {
	return &routing.InvariantError{Path: fmt.Sprintf("connection.%s.%s", provider, field), Message: message}
}

func validateLMStudioExecutionBase(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return fmt.Errorf("must be an absolute HTTP(S) URL ending in /v1")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/v1") {
		return fmt.Errorf("must end in /v1")
	}
	return nil
}
