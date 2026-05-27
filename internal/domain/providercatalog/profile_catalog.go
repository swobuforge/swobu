package providercatalog

import (
	"slices"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type AuthKind string

const (
	AuthNone          AuthKind = "none"
	AuthCredentialRef AuthKind = "credential_ref"
)

type AuthVariant string

const (
	AuthVariantEnv               AuthVariant = "env"
	AuthVariantFile              AuthVariant = "file"
	AuthVariantAWSProfile        AuthVariant = "aws_profile"
	AuthVariantAWSEnvSession     AuthVariant = "aws_env_session"
	AuthVariantChatGPTLogin      AuthVariant = "chatgpt_login"
	AuthVariantChatGPTDeviceAuth AuthVariant = "chatgpt_device_auth"
)

type AuthModeID string

const (
	AuthModeNone               AuthModeID = "none"
	AuthModeTokenEnv           AuthModeID = "token_env"
	AuthModeTokenFile          AuthModeID = "token_file"
	AuthModeAWSProfile         AuthModeID = "aws_profile"
	AuthModeAWSEnvSession      AuthModeID = "aws_env_session"
	AuthModeInteractiveBrowser AuthModeID = "interactive_browser"
	AuthModeInteractiveDevice  AuthModeID = "interactive_device"
)

type AuthModeRequirement string

const (
	AuthModeRequirementAlways                AuthModeRequirement = "always"
	AuthModeRequirementNever                 AuthModeRequirement = "never"
	AuthModeRequirementExceptLoopbackExecute AuthModeRequirement = "except_loopback_execute_origin"
)

type AuthModeSpec struct {
	ID          AuthModeID
	Variant     AuthVariant
	Kind        AuthKind
	Requirement AuthModeRequirement
	Interactive bool
}

type Capability string

const (
	ProviderSpecOllama           ProviderID = "ollama"
	ProviderSpecOpenAI           ProviderID = "openai"
	ProviderSpecChatGPT          ProviderID = "chatgpt"
	ProviderSpecAnthropic        ProviderID = "anthropic"
	ProviderSpecOpenRouter       ProviderID = "openrouter"
	ProviderSpecBedrock          ProviderID = "bedrock"
	ProviderSpecOpenAICompatible ProviderID = "openai_compatible"

	CapabilityModelCatalog Capability = "model_catalog"
	CapabilityStreaming    Capability = "streaming"
)

// Profile is one canonical provider declaration.
//
// Add/remove/evolve provider specs in this catalog only.
type Profile struct {
	ProviderID              ProviderID
	ProviderDisplayName     string
	SetupHint               string
	DefaultBaseURL          string
	DefaultCredentialEnvVar string
	VisibleInOperatorUI     bool
	ProviderProtocols       []ProviderProtocolSpec
	AllowedAuthModes        []AuthModeSpec
	DeclaredCapabilities    []Capability
}

type ProviderProtocolSpec struct {
	Name  string
	Kind  protocolkind.ProtocolKind
	Frame string
}

var (
	providerProtocolsOpenAIFamily = []ProviderProtocolSpec{
		{Name: "responses", Kind: protocolkind.Responses, Frame: FrameHTTPJSONBody},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
		{Name: "chat_completions", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
		{Name: "completions", Kind: protocolkind.Completions, Frame: FrameHTTPJSONBody},
		{Name: "completions_stream", Kind: protocolkind.Completions, Frame: FrameSSEEvent},
	}
	providerProtocolsChatGPT = []ProviderProtocolSpec{
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
	}
	providerProtocolsAnthropic = []ProviderProtocolSpec{
		{Name: "messages", Kind: protocolkind.Messages, Frame: FrameHTTPJSONBody},
		{Name: "messages_stream", Kind: protocolkind.Messages, Frame: FrameSSEEvent},
	}
	providerProtocolsBedrock = []ProviderProtocolSpec{
		{Name: "converse", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody},
		{Name: "converse_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
		{Name: "invoke_model", Kind: protocolkind.Completions, Frame: FrameHTTPJSONBody},
		{Name: "invoke_model_stream", Kind: protocolkind.Completions, Frame: FrameSSEEvent},
	}
)

func catalog() []Profile {
	return []Profile{
		{
			ProviderID:          ProviderSpecOllama,
			ProviderDisplayName: "Ollama",
			SetupHint:           string(ProviderSpecOllama),
			DefaultBaseURL:      "http://127.0.0.1:11434/v1",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsOpenAIFamily),
			AllowedAuthModes: []AuthModeSpec{
				{ID: AuthModeNone, Variant: "", Kind: AuthNone, Requirement: AuthModeRequirementNever},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
		{
			ProviderID:              ProviderSpecOpenAI,
			ProviderDisplayName:     "OpenAI",
			SetupHint:               string(ProviderSpecOpenAI),
			DefaultBaseURL:          "https://api.openai.com/v1",
			DefaultCredentialEnvVar: "OPENAI_API_KEY",
			VisibleInOperatorUI:     true,
			ProviderProtocols:       slices.Clone(providerProtocolsOpenAIFamily),
			AllowedAuthModes: []AuthModeSpec{
				{ID: AuthModeTokenEnv, Variant: AuthVariantEnv, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{ID: AuthModeTokenFile, Variant: AuthVariantFile, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
		{
			ProviderID:          ProviderSpecChatGPT,
			ProviderDisplayName: "ChatGPT",
			SetupHint:           string(ProviderSpecChatGPT),
			DefaultBaseURL:      "https://api.openai.com/v1",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsChatGPT),
			AllowedAuthModes: []AuthModeSpec{
				{ID: AuthModeInteractiveBrowser, Variant: AuthVariantChatGPTLogin, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways, Interactive: true},
				{ID: AuthModeInteractiveDevice, Variant: AuthVariantChatGPTDeviceAuth, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways, Interactive: true},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
		{
			ProviderID:              ProviderSpecAnthropic,
			ProviderDisplayName:     "Anthropic",
			SetupHint:               string(ProviderSpecAnthropic),
			DefaultBaseURL:          "https://api.anthropic.com/v1",
			DefaultCredentialEnvVar: "ANTHROPIC_API_KEY",
			VisibleInOperatorUI:     true,
			ProviderProtocols:       slices.Clone(providerProtocolsAnthropic),
			AllowedAuthModes: []AuthModeSpec{
				{ID: AuthModeTokenEnv, Variant: AuthVariantEnv, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{ID: AuthModeTokenFile, Variant: AuthVariantFile, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
		{
			ProviderID:              ProviderSpecOpenRouter,
			ProviderDisplayName:     "OpenRouter",
			SetupHint:               string(ProviderSpecOpenRouter),
			DefaultBaseURL:          "https://openrouter.ai/api/v1",
			DefaultCredentialEnvVar: "OPENROUTER_API_KEY",
			VisibleInOperatorUI:     true,
			ProviderProtocols:       slices.Clone(providerProtocolsOpenAIFamily),
			AllowedAuthModes: []AuthModeSpec{
				{ID: AuthModeTokenEnv, Variant: AuthVariantEnv, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{ID: AuthModeTokenFile, Variant: AuthVariantFile, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
		{
			ProviderID:              ProviderSpecBedrock,
			ProviderDisplayName:     "AWS Bedrock",
			SetupHint:               string(ProviderSpecBedrock) + "   Bedrock runtime endpoint URL",
			DefaultBaseURL:          "",
			DefaultCredentialEnvVar: "AWS_BEARER_TOKEN_BEDROCK",
			VisibleInOperatorUI:     true,
			ProviderProtocols:       slices.Clone(providerProtocolsBedrock),
			AllowedAuthModes: []AuthModeSpec{
				{ID: AuthModeAWSProfile, Variant: AuthVariantAWSProfile, Kind: AuthNone, Requirement: AuthModeRequirementNever},
				{ID: AuthModeAWSEnvSession, Variant: AuthVariantAWSEnvSession, Kind: AuthNone, Requirement: AuthModeRequirementNever},
				{ID: AuthModeTokenEnv, Variant: AuthVariantEnv, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
			},
			DeclaredCapabilities: []Capability{CapabilityStreaming},
		},
		{
			ProviderID:          ProviderSpecOpenAICompatible,
			ProviderDisplayName: "OpenAI Compatible",
			SetupHint:           string(ProviderSpecOpenAICompatible) + "   OpenAI-compatible URL (https://host/v1)",
			DefaultBaseURL:      "",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsOpenAIFamily),
			AllowedAuthModes: []AuthModeSpec{
				{ID: AuthModeNone, Variant: "", Kind: AuthNone, Requirement: AuthModeRequirementExceptLoopbackExecute},
				{ID: AuthModeTokenEnv, Variant: AuthVariantEnv, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{ID: AuthModeTokenFile, Variant: AuthVariantFile, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
	}
}

func profileFor(spec string) (Profile, bool) {
	normalizedSpec := strings.TrimSpace(spec) // swobu:io-string source=domain
	if normalizedSpec == "" {
		return Profile{}, false
	}
	for _, profile := range catalog() {
		if string(profile.ProviderID) == normalizedSpec {
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
		specs = append(specs, string(entry.ProviderID))
	}
	slices.Sort(specs)
	return specs
}

func SupportsSpec(spec string) bool {
	_, ok := profileFor(spec)
	return ok
}

func SupportsAuth(spec string, authKind AuthKind) bool {
	for _, mode := range AllowedAuthModesForSpec(spec) {
		supported := mode.Kind
		if supported == authKind {
			return true
		}
	}
	return false
}

func AllowedAuthModesForSpec(spec string) []AuthModeSpec {
	profile, ok := profileFor(spec)
	if !ok {
		return nil
	}
	return slices.Clone(profile.AllowedAuthModes)
}

func SupportedAuthVariantsForSpec(spec string) []AuthVariant {
	modes := AllowedAuthModesForSpec(spec)
	out := make([]AuthVariant, 0, len(modes))
	for _, mode := range modes {
		variant := mode.Variant
		if strings.TrimSpace(string(variant)) == "" { // swobu:io-string source=domain
			continue
		}
		out = append(out, variant)
	}
	return slices.Compact(out)
}

func SupportsAuthVariant(spec string, variant AuthVariant) bool {
	for _, supported := range SupportedAuthVariantsForSpec(spec) {
		if supported == variant {
			return true
		}
	}
	return false
}

func IsInteractiveAuthVariant(variant AuthVariant) bool {
	switch variant {
	case AuthVariantChatGPTLogin, AuthVariantChatGPTDeviceAuth:
		return true
	default:
		return false
	}
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

func DefaultExecuteBaseURL(spec string) string {
	profile, ok := profileFor(spec)
	if !ok {
		return ""
	}
	return profile.DefaultBaseURL
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
