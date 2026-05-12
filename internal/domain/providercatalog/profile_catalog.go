package providercatalog

import (
	"slices"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
)

const (
	AdapterCustomOpenAICompatible = "custom_openai_compatible"
	AdapterAnthropicMessages      = "anthropic_messages"
)

type AuthKind string

const (
	AuthNone          AuthKind = "none"
	AuthCredentialRef AuthKind = "credential_ref"
)

type AuthVariant string

const (
	AuthVariantEnv               AuthVariant = "env"
	AuthVariantKeychain          AuthVariant = "keychain"
	AuthVariantFile              AuthVariant = "file"
	AuthVariantChatGPTLogin      AuthVariant = "chatgpt_login"
	AuthVariantChatGPTDeviceAuth AuthVariant = "chatgpt_device_auth"
)

type APIFamily string

const (
	APIFamilyOpenAICompatible APIFamily = "openai_compatible"
	APIFamilyAnthropic        APIFamily = "anthropic_api"

	// Backward-compatible aliases; new code should prefer APIFamily* names.
	EndpointModeOpenAICompatible = APIFamilyOpenAICompatible
	EndpointModeAnthropic        = APIFamilyAnthropic
)

type Capability string

const (
	CapabilityModelCatalog Capability = "model_catalog"
	CapabilityStreaming    Capability = "streaming"
)

type credentialRequirementPolicy uint8

const (
	credentialRequiredAlways credentialRequirementPolicy = iota
	credentialNeverRequired
	credentialRequiredExceptLoopbackCustom
)

// Profile is one canonical provider declaration.
//
// Add/remove/evolve provider specs in this catalog only.
type Profile struct {
	ProviderID               string
	ProviderDisplayName      string
	SetupHint                string
	DefaultBaseURL           string
	DefaultCredentialEnvVar  string
	HasModelCatalog          bool
	VisibleInOperatorUI      bool
	ExecutionAdapterID       string
	APIFamily                APIFamily
	SupportedCredentialKinds []AuthKind
	SupportedAuthVariants    []AuthVariant
	DeclaredCapabilities     []Capability
	SupportedEgressProtocols []protocolsurface.Kind

	credentialRequirementPolicy credentialRequirementPolicy
}

func catalog() []Profile {
	return []Profile{
		{
			ProviderID:               "ollama",
			ProviderDisplayName:      "Ollama",
			SetupHint:                "ollama",
			DefaultBaseURL:           "http://127.0.0.1:11434/v1",
			HasModelCatalog:          true,
			VisibleInOperatorUI:      true,
			ExecutionAdapterID:       AdapterCustomOpenAICompatible,
			APIFamily:                APIFamilyOpenAICompatible,
			SupportedCredentialKinds: []AuthKind{AuthNone},
			SupportedAuthVariants:    []AuthVariant{},
			DeclaredCapabilities:     []Capability{CapabilityModelCatalog, CapabilityStreaming},
			SupportedEgressProtocols: []protocolsurface.Kind{
				protocolsurface.ChatCompletions,
				protocolsurface.Responses,
				protocolsurface.Completions,
			},
			credentialRequirementPolicy: credentialNeverRequired,
		},
		{
			ProviderID:               "openai",
			ProviderDisplayName:      "OpenAI",
			SetupHint:                "openai",
			DefaultBaseURL:           "https://api.openai.com/v1",
			DefaultCredentialEnvVar:  "OPENAI_API_KEY",
			HasModelCatalog:          true,
			VisibleInOperatorUI:      true,
			ExecutionAdapterID:       AdapterCustomOpenAICompatible,
			APIFamily:                APIFamilyOpenAICompatible,
			SupportedCredentialKinds: []AuthKind{AuthCredentialRef},
			SupportedAuthVariants:    []AuthVariant{AuthVariantEnv, AuthVariantKeychain, AuthVariantFile},
			DeclaredCapabilities:     []Capability{CapabilityModelCatalog, CapabilityStreaming},
			SupportedEgressProtocols: []protocolsurface.Kind{
				protocolsurface.ChatCompletions,
				protocolsurface.Responses,
				protocolsurface.Completions,
			},
			credentialRequirementPolicy: credentialRequiredAlways,
		},
		{
			ProviderID:               "chatgpt",
			ProviderDisplayName:      "ChatGPT",
			SetupHint:                "chatgpt",
			DefaultBaseURL:           "https://api.openai.com/v1",
			HasModelCatalog:          true,
			VisibleInOperatorUI:      true,
			ExecutionAdapterID:       AdapterCustomOpenAICompatible,
			APIFamily:                APIFamilyOpenAICompatible,
			SupportedCredentialKinds: []AuthKind{AuthCredentialRef},
			SupportedAuthVariants:    []AuthVariant{AuthVariantChatGPTLogin, AuthVariantChatGPTDeviceAuth},
			DeclaredCapabilities:     []Capability{CapabilityModelCatalog, CapabilityStreaming},
			SupportedEgressProtocols: []protocolsurface.Kind{
				protocolsurface.ChatCompletions,
				protocolsurface.Responses,
				protocolsurface.Completions,
			},
			credentialRequirementPolicy: credentialRequiredAlways,
		},
		{
			ProviderID:               "anthropic",
			ProviderDisplayName:      "Anthropic",
			SetupHint:                "anthropic",
			DefaultBaseURL:           "https://api.anthropic.com/v1",
			DefaultCredentialEnvVar:  "ANTHROPIC_API_KEY",
			HasModelCatalog:          true,
			VisibleInOperatorUI:      true,
			ExecutionAdapterID:       AdapterAnthropicMessages,
			APIFamily:                APIFamilyAnthropic,
			SupportedCredentialKinds: []AuthKind{AuthCredentialRef},
			SupportedAuthVariants:    []AuthVariant{AuthVariantEnv, AuthVariantKeychain, AuthVariantFile},
			DeclaredCapabilities:     []Capability{CapabilityModelCatalog, CapabilityStreaming},
			SupportedEgressProtocols: []protocolsurface.Kind{
				protocolsurface.Messages,
			},
			credentialRequirementPolicy: credentialRequiredAlways,
		},
		{
			ProviderID:               "openrouter",
			ProviderDisplayName:      "OpenRouter",
			SetupHint:                "openrouter",
			DefaultBaseURL:           "https://openrouter.ai/api/v1",
			DefaultCredentialEnvVar:  "OPENROUTER_API_KEY",
			HasModelCatalog:          true,
			VisibleInOperatorUI:      true,
			ExecutionAdapterID:       AdapterCustomOpenAICompatible,
			APIFamily:                APIFamilyOpenAICompatible,
			SupportedCredentialKinds: []AuthKind{AuthCredentialRef},
			SupportedAuthVariants:    []AuthVariant{AuthVariantEnv, AuthVariantKeychain, AuthVariantFile},
			DeclaredCapabilities:     []Capability{CapabilityModelCatalog, CapabilityStreaming},
			SupportedEgressProtocols: []protocolsurface.Kind{
				protocolsurface.ChatCompletions,
				protocolsurface.Responses,
				protocolsurface.Completions,
			},
			credentialRequirementPolicy: credentialRequiredAlways,
		},
		{
			ProviderID:               "custom",
			ProviderDisplayName:      "Custom",
			SetupHint:                "custom   openai-compatible URL (https://host/v1)",
			DefaultBaseURL:           "",
			HasModelCatalog:          true,
			VisibleInOperatorUI:      true,
			ExecutionAdapterID:       AdapterCustomOpenAICompatible,
			APIFamily:                APIFamilyOpenAICompatible,
			SupportedCredentialKinds: []AuthKind{AuthNone, AuthCredentialRef},
			SupportedAuthVariants:    []AuthVariant{AuthVariantEnv, AuthVariantKeychain, AuthVariantFile},
			DeclaredCapabilities:     []Capability{CapabilityModelCatalog, CapabilityStreaming},
			SupportedEgressProtocols: []protocolsurface.Kind{
				protocolsurface.ChatCompletions,
				protocolsurface.Responses,
				protocolsurface.Completions,
			},
			credentialRequirementPolicy: credentialRequiredExceptLoopbackCustom,
		},
	}
}

func profileFor(spec string) (Profile, bool) {
	spec = strings.TrimSpace(strings.ToLower(spec))
	for _, profile := range catalog() {
		if profile.ProviderID == spec {
			return profile, true
		}
	}
	return Profile{}, false
}

func All() []Profile {
	return slices.Clone(catalog())
}

func SupportedSpecs() []string {
	entries := catalog()
	specs := make([]string, 0, len(entries))
	for _, entry := range entries {
		specs = append(specs, entry.ProviderID)
	}
	slices.Sort(specs)
	return specs
}

func SupportsSpec(spec string) bool {
	_, ok := profileFor(spec)
	return ok
}

func SupportsRoute(spec string, protocolKind protocolsurface.Kind) bool {
	profile, ok := profileFor(spec)
	if !ok {
		return false
	}
	for _, supported := range profile.SupportedEgressProtocols {
		if supported == protocolKind {
			return true
		}
	}
	return false
}

// DefaultProtocolForSpec returns the canonical default protocol kind for one
// provider spec.
func DefaultProtocolForSpec(spec string) (protocolsurface.Kind, bool) {
	profile, ok := profileFor(spec)
	if !ok || len(profile.SupportedEgressProtocols) == 0 {
		return "", false
	}
	return profile.SupportedEgressProtocols[0], true
}

func SupportsAuth(spec string, authKind AuthKind) bool {
	profile, ok := profileFor(spec)
	if !ok {
		return false
	}
	for _, supported := range profile.SupportedCredentialKinds {
		if supported == authKind {
			return true
		}
	}
	return false
}

func SupportedAuthVariantsForSpec(spec string) []AuthVariant {
	profile, ok := profileFor(spec)
	if !ok {
		return nil
	}
	return slices.Clone(profile.SupportedAuthVariants)
}

func SupportsAuthVariant(spec string, variant AuthVariant) bool {
	profile, ok := profileFor(spec)
	if !ok {
		return false
	}
	for _, supported := range profile.SupportedAuthVariants {
		if supported == variant {
			return true
		}
	}
	return false
}

func IsInteractiveAuthVariant(variant AuthVariant) bool {
	return variant == AuthVariantChatGPTLogin || variant == AuthVariantChatGPTDeviceAuth
}

func AuthVariantStartAction(spec string, variant AuthVariant) (label string, verb string, ok bool) {
	if !SupportsAuthVariant(spec, variant) || !IsInteractiveAuthVariant(variant) {
		return "", "", false
	}
	switch variant {
	case AuthVariantChatGPTDeviceAuth:
		return "start device auth", "start", true
	case AuthVariantChatGPTLogin:
		return "start login", "login", true
	default:
		return "start login", "login", true
	}
}

func AuthVariantDisplayLabel(spec string, variant AuthVariant) string {
	switch variant {
	case AuthVariantChatGPTLogin:
		return "browser login"
	case AuthVariantChatGPTDeviceAuth:
		return "device code"
	case AuthVariantEnv:
		return "env var"
	case AuthVariantKeychain:
		return "keychain"
	case AuthVariantFile:
		return "file"
	default:
		return string(variant)
	}
}

func SupportsEndpointMode(spec string, endpointMode APIFamily) bool {
	profile, ok := profileFor(spec)
	return ok && profile.APIFamily == endpointMode
}

func SupportsCapability(spec string, capability Capability) bool {
	profile, ok := profileFor(spec)
	if !ok {
		return false
	}
	for _, supported := range profile.DeclaredCapabilities {
		if supported == capability {
			return true
		}
	}
	return false
}

func ProviderDisplayName(spec string) string {
	profile, ok := profileFor(spec)
	if !ok {
		return "Provider"
	}
	return profile.ProviderDisplayName
}

// TODO(execution-system): DisplayName is a compatibility wrapper. New code should call ProviderDisplayName.
func DisplayName(spec string) string {
	return ProviderDisplayName(spec)
}

func DefaultBaseURL(spec string) string {
	profile, ok := profileFor(spec)
	if !ok {
		return ""
	}
	return profile.DefaultBaseURL
}

func AdapterForSpec(spec string) (string, bool) {
	profile, ok := profileFor(spec)
	if !ok {
		return "", false
	}
	return profile.ExecutionAdapterID, true
}

func EndpointModeForSpec(spec string) (APIFamily, bool) {
	profile, ok := profileFor(spec)
	if !ok {
		return "", false
	}
	return profile.APIFamily, true
}

// DefaultEnvKeyForSpec returns the canonical environment variable name for a
// provider spec, or empty if the provider has no stable env key convention.
func DefaultEnvKeyForSpec(spec string) string {
	profile, ok := profileFor(spec)
	if !ok {
		return ""
	}
	return profile.DefaultCredentialEnvVar
}

// DefaultCredentialEnvVarForSpec returns the canonical environment variable name
// for credential lookup, or empty if none is declared for the provider.
func DefaultCredentialEnvVarForSpec(spec string) string {
	return DefaultEnvKeyForSpec(spec)
}

func RequiresCredential(spec, baseURL string) bool {
	profile, ok := profileFor(spec)
	if !ok {
		return false
	}
	switch profile.credentialRequirementPolicy {
	case credentialNeverRequired:
		return false
	case credentialRequiredExceptLoopbackCustom:
		normalizedBaseURL := strings.TrimSpace(strings.ToLower(baseURL))
		return !(strings.HasPrefix(normalizedBaseURL, "http://127.0.0.1") || strings.HasPrefix(normalizedBaseURL, "http://localhost"))
	default:
		return true
	}
}

func InferAuthKind(spec, baseURL, credentialRef string) AuthKind {
	if strings.TrimSpace(credentialRef) != "" {
		return AuthCredentialRef
	}
	if RequiresCredential(spec, baseURL) {
		return AuthCredentialRef
	}
	return AuthNone
}

func HasModelCatalog(spec string) bool {
	profile, ok := profileFor(spec)
	return ok && profile.HasModelCatalog
}

// TODO(execution-system): SupportsModelCatalog is a compatibility wrapper. New code should call HasModelCatalog.
func SupportsModelCatalog(spec string) bool {
	return HasModelCatalog(spec)
}

type RouteProfile struct {
	ProviderSpec string
	// ProtocolKind is the concrete provider-side egress protocol family that the
	// selected adapter will encode to for this target.
	ProtocolKind       protocolsurface.Kind
	AuthKind           AuthKind
	APIFamily          APIFamily
	ExecutionAdapterID string
}

// ResolveRouteProfile resolves one execution-route profile from durable target
// intent.
//
// The protocolKind input is an egress codec selection for the provider target,
// not an ingress-family discriminator.
func ResolveRouteProfile(spec string, protocolKind protocolsurface.Kind, baseURL, credentialRef string) (RouteProfile, bool) {
	spec = strings.TrimSpace(strings.ToLower(spec))
	if !SupportsSpec(spec) {
		return RouteProfile{}, false
	}
	if !SupportsRoute(spec, protocolKind) {
		return RouteProfile{}, false
	}
	endpointMode, ok := EndpointModeForSpec(spec)
	if !ok {
		return RouteProfile{}, false
	}
	authKind := InferAuthKind(spec, baseURL, credentialRef)
	if !SupportsAuth(spec, authKind) {
		return RouteProfile{}, false
	}
	adapter, ok := AdapterForSpec(spec)
	if !ok {
		return RouteProfile{}, false
	}
	return RouteProfile{
		ProviderSpec:       spec,
		ProtocolKind:       protocolKind,
		AuthKind:           authKind,
		APIFamily:          endpointMode,
		ExecutionAdapterID: adapter,
	}, true
}
