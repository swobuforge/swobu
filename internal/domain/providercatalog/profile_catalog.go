package providercatalog

import (
	"slices"
	"strings"

	"github.com/metrofun/swobu/internal/domain/protocolsurface"
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

type EndpointMode string

const (
	EndpointModeOpenAICompatible EndpointMode = "openai_compatible"
	EndpointModeAnthropic        EndpointMode = "anthropic_api"
)

type Capability string

const (
	CapabilityModelCatalog Capability = "model_catalog"
	CapabilityStreaming    Capability = "streaming"
)

type credentialPolicy uint8

const (
	credentialAlways credentialPolicy = iota
	credentialNever
	credentialCustomBaseURL
)

// Profile is one canonical provider declaration.
//
// Add/remove/evolve provider specs in this catalog only.
type Profile struct {
	Spec                 string
	DisplayName          string
	OperatorSetupLabel   string
	DefaultBaseURL       string
	DefaultEnvKey        string
	SupportsModelCatalog bool
	OperatorVisible      bool
	Adapter              string
	EndpointMode         EndpointMode
	SupportedAuthKinds   []AuthKind
	Capabilities         []Capability
	SupportedProtocols   []protocolsurface.Kind

	credentialPolicy credentialPolicy
}

func catalog() []Profile {
	return []Profile{
		{
			Spec:                 "openai",
			DisplayName:          "OpenAI",
			OperatorSetupLabel:   "openai",
			DefaultBaseURL:       "https://api.openai.com/v1",
			DefaultEnvKey:        "OPENAI_API_KEY",
			SupportsModelCatalog: true,
			OperatorVisible:      true,
			Adapter:              AdapterCustomOpenAICompatible,
			EndpointMode:         EndpointModeOpenAICompatible,
			SupportedAuthKinds:   []AuthKind{AuthCredentialRef},
			Capabilities:         []Capability{CapabilityModelCatalog, CapabilityStreaming},
			SupportedProtocols: []protocolsurface.Kind{
				protocolsurface.ChatCompletions,
				protocolsurface.Responses,
				protocolsurface.Completions,
			},
			credentialPolicy: credentialAlways,
		},
		{
			Spec:                 "openrouter",
			DisplayName:          "OpenRouter",
			OperatorSetupLabel:   "openrouter",
			DefaultBaseURL:       "https://openrouter.ai/api/v1",
			DefaultEnvKey:        "OPENROUTER_API_KEY",
			SupportsModelCatalog: true,
			OperatorVisible:      true,
			Adapter:              AdapterCustomOpenAICompatible,
			EndpointMode:         EndpointModeOpenAICompatible,
			SupportedAuthKinds:   []AuthKind{AuthCredentialRef},
			Capabilities:         []Capability{CapabilityModelCatalog, CapabilityStreaming},
			SupportedProtocols: []protocolsurface.Kind{
				protocolsurface.ChatCompletions,
				protocolsurface.Responses,
				protocolsurface.Completions,
			},
			credentialPolicy: credentialAlways,
		},
		{
			Spec:                 "anthropic",
			DisplayName:          "Anthropic",
			OperatorSetupLabel:   "anthropic",
			DefaultBaseURL:       "https://api.anthropic.com/v1",
			DefaultEnvKey:        "ANTHROPIC_API_KEY",
			SupportsModelCatalog: true,
			OperatorVisible:      true,
			Adapter:              AdapterAnthropicMessages,
			EndpointMode:         EndpointModeAnthropic,
			SupportedAuthKinds:   []AuthKind{AuthCredentialRef},
			Capabilities:         []Capability{CapabilityModelCatalog, CapabilityStreaming},
			SupportedProtocols: []protocolsurface.Kind{
				protocolsurface.Messages,
			},
			credentialPolicy: credentialAlways,
		},
		{
			Spec:                 "ollama",
			DisplayName:          "Ollama",
			OperatorSetupLabel:   "ollama",
			DefaultBaseURL:       "http://127.0.0.1:11434/v1",
			SupportsModelCatalog: true,
			OperatorVisible:      true,
			Adapter:              AdapterCustomOpenAICompatible,
			EndpointMode:         EndpointModeOpenAICompatible,
			SupportedAuthKinds:   []AuthKind{AuthNone},
			Capabilities:         []Capability{CapabilityModelCatalog, CapabilityStreaming},
			SupportedProtocols: []protocolsurface.Kind{
				protocolsurface.ChatCompletions,
				protocolsurface.Responses,
				protocolsurface.Completions,
			},
			credentialPolicy: credentialNever,
		},
		{
			Spec:                 "custom",
			DisplayName:          "Custom",
			OperatorSetupLabel:   "custom   openai-compatible URL (https://host/v1)",
			DefaultBaseURL:       "",
			SupportsModelCatalog: true,
			OperatorVisible:      true,
			Adapter:              AdapterCustomOpenAICompatible,
			EndpointMode:         EndpointModeOpenAICompatible,
			SupportedAuthKinds:   []AuthKind{AuthNone, AuthCredentialRef},
			Capabilities:         []Capability{CapabilityModelCatalog, CapabilityStreaming},
			SupportedProtocols: []protocolsurface.Kind{
				protocolsurface.ChatCompletions,
				protocolsurface.Responses,
				protocolsurface.Completions,
			},
			credentialPolicy: credentialCustomBaseURL,
		},
	}
}

func profileFor(spec string) (Profile, bool) {
	spec = strings.TrimSpace(strings.ToLower(spec))
	for _, profile := range catalog() {
		if profile.Spec == spec {
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
		specs = append(specs, entry.Spec)
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
	for _, supported := range profile.SupportedProtocols {
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
	if !ok || len(profile.SupportedProtocols) == 0 {
		return "", false
	}
	return profile.SupportedProtocols[0], true
}

func SupportsAuth(spec string, authKind AuthKind) bool {
	profile, ok := profileFor(spec)
	if !ok {
		return false
	}
	for _, supported := range profile.SupportedAuthKinds {
		if supported == authKind {
			return true
		}
	}
	return false
}

func SupportsEndpointMode(spec string, endpointMode EndpointMode) bool {
	profile, ok := profileFor(spec)
	return ok && profile.EndpointMode == endpointMode
}

func SupportsCapability(spec string, capability Capability) bool {
	profile, ok := profileFor(spec)
	if !ok {
		return false
	}
	for _, supported := range profile.Capabilities {
		if supported == capability {
			return true
		}
	}
	return false
}

func DisplayName(spec string) string {
	profile, ok := profileFor(spec)
	if !ok {
		return "Provider"
	}
	return profile.DisplayName
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
	return profile.Adapter, true
}

func EndpointModeForSpec(spec string) (EndpointMode, bool) {
	profile, ok := profileFor(spec)
	if !ok {
		return "", false
	}
	return profile.EndpointMode, true
}

// DefaultEnvKeyForSpec returns the canonical environment variable name for a
// provider spec, or empty if the provider has no stable env key convention.
func DefaultEnvKeyForSpec(spec string) string {
	profile, ok := profileFor(spec)
	if !ok {
		return ""
	}
	return profile.DefaultEnvKey
}

func RequiresCredential(spec, baseURL string) bool {
	profile, ok := profileFor(spec)
	if !ok {
		return false
	}
	switch profile.credentialPolicy {
	case credentialNever:
		return false
	case credentialCustomBaseURL:
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

func SupportsModelCatalog(spec string) bool {
	profile, ok := profileFor(spec)
	return ok && profile.SupportsModelCatalog
}

type RouteProfile struct {
	ProviderSpec string
	ProtocolKind protocolsurface.Kind
	AuthKind     AuthKind
	EndpointMode EndpointMode
	Adapter      string
}

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
		ProviderSpec: spec,
		ProtocolKind: protocolKind,
		AuthKind:     authKind,
		EndpointMode: endpointMode,
		Adapter:      adapter,
	}, true
}
