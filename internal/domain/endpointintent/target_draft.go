package endpointintent

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
)

// TargetDraft is the Cockpit save-boundary declaration for one route target.
// It is not durable config; NewProviderConfigFromTargetDraft validates and
// normalizes it into ProviderConfig.
type TargetDraft struct {
	ProviderSpec string

	Endpoint ProviderEndpointDraft

	CredentialRef string

	ProviderProtocol string
	ModelID          string
	RouteModelID     string

	Rank   int
	Weight int

	ProviderOptions ProviderOptionsDraft
}

type ProviderEndpointKind string

const (
	EndpointDefaultHTTPBaseURL   ProviderEndpointKind = "default_http_base_url"
	EndpointRequiredHTTPBaseURL  ProviderEndpointKind = "required_http_base_url"
	EndpointAzureResourceLocator ProviderEndpointKind = "azure_resource_locator"
)

// ProviderEndpointKindFromProfile translates provider-catalog endpoint facts
// into the draft enum used by endpoint intent finalization.
func ProviderEndpointKindFromProfile(kind profile.ProviderEndpointKind) (ProviderEndpointKind, bool) {
	switch kind {
	case profile.EndpointDefaultHTTPBaseURL:
		return EndpointDefaultHTTPBaseURL, true
	case profile.EndpointRequiredHTTPBaseURL:
		return EndpointRequiredHTTPBaseURL, true
	case profile.EndpointAzureResourceLocator:
		return EndpointAzureResourceLocator, true
	default:
		return "", false
	}
}

type ProviderEndpointDraft struct {
	Kind  ProviderEndpointKind
	Value string
}

type ProviderOptionsDraft struct {
	OpenAICompatible OpenAICompatibleOptionsDraft
	Bedrock          BedrockOptionsDraft
}

func (o ProviderOptionsDraft) IsEmpty() bool {
	return o.OpenAICompatible.IsEmpty() && o.Bedrock.IsEmpty()
}

type OpenAICompatibleOptionsDraft struct {
	CredentialHeader string
}

func (o OpenAICompatibleOptionsDraft) IsEmpty() bool {
	return strings.TrimSpace(o.CredentialHeader) == ""
}

type BedrockOptionsDraft struct {
	AuthMode string
	// Region is the typed Mantle region seam; the endpoint is derived from it.
	Region string
	// ProfileName is the typed AWS profile seam for aws_profile Bedrock auth.
	ProfileName string
}

func (o BedrockOptionsDraft) IsEmpty() bool {
	return strings.TrimSpace(o.AuthMode) == "" && strings.TrimSpace(o.Region) == "" && strings.TrimSpace(o.ProfileName) == ""
}

func NewProviderConfigFromTargetDraft(ref ProviderConfigRef, draft TargetDraft) (ProviderConfig, error) {
	spec, err := ParseProviderSpec(draft.ProviderSpec)
	if err != nil {
		return ProviderConfig{}, err
	}

	endpointDraft := draft.Endpoint
	if spec.String() == string(profile.ProviderSpecBedrock) &&
		strings.TrimSpace(endpointDraft.Value) == "" &&
		strings.TrimSpace(draft.ProviderOptions.Bedrock.Region) != "" {
		endpointDraft.Value = profile.BedrockMantleEndpointForRegion(draft.ProviderOptions.Bedrock.Region)
	}
	endpointValue, err := finalizeEndpointValue(spec, endpointDraft)
	if err != nil {
		return ProviderConfig{}, err
	}

	credentialRef := strings.TrimSpace(draft.CredentialRef) // swobu:io-string source=boundary
	if spec.String() == string(profile.ProviderSpecBedrock) &&
		strings.TrimSpace(draft.ProviderOptions.Bedrock.AuthMode) == string(profile.AuthModeAWSProfile) {
		profileName := strings.TrimSpace(draft.ProviderOptions.Bedrock.ProfileName)
		if profileName == "" {
			profileName = profile.BedrockProfileNameFromCredentialRef(credentialRef)
		}
		if profileName == "" {
			return ProviderConfig{}, fmt.Errorf("%w: Bedrock profile name is required for auth mode %q", ErrInvalidProviderConfig, string(profile.AuthModeAWSProfile))
		}
		if credentialRef != "" {
			credentialProfile := profile.BedrockProfileNameFromCredentialRef(credentialRef)
			if credentialProfile == "" {
				return ProviderConfig{}, fmt.Errorf("%w: credential ref is unsupported for Bedrock auth mode %q", ErrInvalidProviderConfig, string(profile.AuthModeAWSProfile))
			}
			if credentialProfile != profileName {
				return ProviderConfig{}, fmt.Errorf("%w: credential ref %q does not match Bedrock profile name %q", ErrInvalidProviderConfig, credentialRef, profileName)
			}
		}
		credentialRef = profile.BedrockProfileCredentialRef(profileName)
	}
	if profile.RequiresCredential(spec.String(), endpointValue) && credentialRef == "" {
		return ProviderConfig{}, fmt.Errorf("%w: credential ref is required for provider %q", ErrInvalidProviderConfig, spec.String())
	}

	config, err := NewProviderConfig(ref, spec, endpointValue, credentialRef)
	if err != nil {
		return ProviderConfig{}, err
	}

	if spec.String() == string(profile.ProviderSpecOpenAICompatible) {
		if !draft.ProviderOptions.Bedrock.IsEmpty() {
			return ProviderConfig{}, fmt.Errorf("%w: Bedrock options are unsupported for provider %q", ErrInvalidProviderConfig, spec.String())
		}
		config, err = config.WithAuthHeader(draft.ProviderOptions.OpenAICompatible.CredentialHeader)
		if err != nil {
			return ProviderConfig{}, err
		}
	} else if spec.String() == string(profile.ProviderSpecBedrock) {
		if !draft.ProviderOptions.OpenAICompatible.IsEmpty() {
			return ProviderConfig{}, fmt.Errorf("%w: OpenAI-compatible options are unsupported for provider %q", ErrInvalidProviderConfig, spec.String())
		}
		config, err = finalizeBedrockOptions(config, draft.ProviderOptions.Bedrock, credentialRef, endpointValue)
		if err != nil {
			return ProviderConfig{}, err
		}
	} else if !draft.ProviderOptions.IsEmpty() {
		return ProviderConfig{}, fmt.Errorf("%w: provider options are unsupported for provider %q", ErrInvalidProviderConfig, spec.String())
	}

	protocol := strings.TrimSpace(draft.ProviderProtocol) // swobu:io-string source=boundary
	if protocol == "" {
		return ProviderConfig{}, fmt.Errorf("%w: provider protocol is required", ErrInvalidProviderConfig)
	}
	config, err = config.WithProviderProtocol(protocol)
	if err != nil {
		return ProviderConfig{}, err
	}
	config, err = config.WithRouteModelID(draft.RouteModelID)
	if err != nil {
		return ProviderConfig{}, err
	}
	config, err = config.WithModelID(draft.ModelID)
	if err != nil {
		return ProviderConfig{}, err
	}
	config, err = config.WithTargetRank(draft.Rank)
	if err != nil {
		return ProviderConfig{}, err
	}
	config, err = config.WithTargetWeight(draft.Weight)
	if err != nil {
		return ProviderConfig{}, err
	}
	return config, nil
}

func finalizeBedrockOptions(config ProviderConfig, options BedrockOptionsDraft, credentialRef string, endpointValue string) (ProviderConfig, error) {
	authMode := strings.TrimSpace(options.AuthMode) // swobu:io-string source=boundary
	if authMode == "" {
		return ProviderConfig{}, fmt.Errorf("%w: Bedrock auth mode is required", ErrInvalidProviderConfig)
	}
	if profileName := strings.TrimSpace(options.ProfileName); profileName != "" && authMode != string(profile.AuthModeAWSProfile) {
		return ProviderConfig{}, fmt.Errorf("%w: Bedrock profile name is unsupported for auth mode %q", ErrInvalidProviderConfig, authMode)
	}
	region := strings.TrimSpace(options.Region)
	if region == "" {
		region = profile.BedrockMantleRegionFromEndpoint(endpointValue)
	}
	if region == "" {
		return ProviderConfig{}, fmt.Errorf("%w: Bedrock region is required", ErrInvalidProviderConfig)
	}
	if !profile.SupportsBedrockMantleRegion(region) {
		return ProviderConfig{}, fmt.Errorf("%w: Bedrock region %q is unsupported", ErrInvalidProviderConfig, region)
	}
	derivedEndpoint := profile.BedrockMantleEndpointForRegion(region)
	if strings.TrimSpace(endpointValue) != "" && strings.TrimSpace(endpointValue) != derivedEndpoint {
		return ProviderConfig{}, fmt.Errorf("%w: Bedrock endpoint %q does not match region %q", ErrInvalidProviderConfig, endpointValue, region)
	}
	requiresCredential, ok := profile.AuthModeRequiresCredentialForSpec(
		string(profile.ProviderSpecBedrock),
		profile.AuthMode(authMode),
		derivedEndpoint,
	)
	if !ok {
		return ProviderConfig{}, fmt.Errorf("%w: Bedrock auth mode %q is unsupported", ErrInvalidProviderConfig, authMode)
	}
	if authMode == string(profile.AuthModeAWSProfile) {
		profileName := strings.TrimSpace(options.ProfileName)
		if profileName == "" {
			profileName = profile.BedrockProfileNameFromCredentialRef(credentialRef)
		}
		if profileName == "" {
			return ProviderConfig{}, fmt.Errorf("%w: Bedrock profile name is required for auth mode %q", ErrInvalidProviderConfig, authMode)
		}
		if credentialRef != "" && profile.BedrockProfileNameFromCredentialRef(credentialRef) == "" {
			return ProviderConfig{}, fmt.Errorf("%w: credential ref is unsupported for Bedrock auth mode %q", ErrInvalidProviderConfig, authMode)
		}
		if credentialRef != "" && profile.BedrockProfileNameFromCredentialRef(credentialRef) != "" && profile.BedrockProfileNameFromCredentialRef(credentialRef) != profileName {
			return ProviderConfig{}, fmt.Errorf("%w: credential ref %q does not match Bedrock profile name %q", ErrInvalidProviderConfig, credentialRef, profileName)
		}
		return config.WithAuthMode(authMode)
	}
	if requiresCredential && strings.TrimSpace(credentialRef) == "" {
		return ProviderConfig{}, fmt.Errorf("%w: credential ref is required for Bedrock auth mode %q", ErrInvalidProviderConfig, authMode)
	}
	if !requiresCredential && strings.TrimSpace(credentialRef) != "" {
		return ProviderConfig{}, fmt.Errorf("%w: credential ref is unsupported for Bedrock auth mode %q", ErrInvalidProviderConfig, authMode)
	}
	return config.WithAuthMode(authMode)
}

func finalizeEndpointValue(spec ProviderSpec, endpoint ProviderEndpointDraft) (string, error) {
	endpointSpec, ok := profile.EndpointSpecForProvider(spec.String())
	if !ok {
		return "", fmt.Errorf("%w: provider %q has no endpoint spec", ErrInvalidProviderConfig, spec.String())
	}
	expectedKind, ok := ProviderEndpointKindFromProfile(endpointSpec.Kind)
	if !ok {
		return "", fmt.Errorf("%w: endpoint kind %q is unsupported", ErrInvalidProviderConfig, endpointSpec.Kind)
	}
	if endpoint.Kind == "" {
		endpoint.Kind = expectedKind
	}
	if endpoint.Kind != expectedKind {
		return "", fmt.Errorf(
			"%w: endpoint kind %q is unsupported for provider %q",
			ErrInvalidProviderConfig,
			endpoint.Kind,
			spec.String(),
		)
	}
	value := strings.TrimSpace(endpoint.Value) // swobu:io-string source=boundary
	switch expectedKind {
	case EndpointDefaultHTTPBaseURL:
		if value == "" {
			value = strings.TrimSpace(endpointSpec.DefaultURL)
		}
		if value == "" {
			return "", fmt.Errorf("%w: provider %q has no default endpoint URL", ErrInvalidProviderConfig, spec.String())
		}
		return value, nil
	case EndpointRequiredHTTPBaseURL:
		if value == "" {
			return "", fmt.Errorf("%w: endpoint URL is required for provider %q", ErrInvalidProviderConfig, spec.String())
		}
		return value, nil
	case EndpointAzureResourceLocator:
		if value == "" {
			return "", fmt.Errorf("%w: Azure project endpoint is required", ErrInvalidProviderConfig)
		}
		return value, nil
	}
	return "", fmt.Errorf("%w: endpoint kind %q is unsupported", ErrInvalidProviderConfig, endpointSpec.Kind)
}
