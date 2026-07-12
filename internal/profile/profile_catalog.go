package profile

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

// AuthMode names one provider auth path in the catalog.
//
// It is the canonical selector for credential-ref sources (env/file/keychain),
// ambient AWS auth, and interactive login/device flows. It is not a storage
// source type.
type AuthMode string

const (
	AuthModeEnv               AuthMode = "env"
	AuthModeFile              AuthMode = "file"
	AuthModeKeychain          AuthMode = "keychain"
	AuthModeAWSProfile        AuthMode = "aws_profile"
	AuthModeAWSEnvSession     AuthMode = "aws_env_session"
	AuthModeChatGPTLogin      AuthMode = "chatgpt_login"
	AuthModeChatGPTDeviceAuth AuthMode = "chatgpt_device_auth"
)

type AuthModeRequirement string

const (
	AuthModeRequirementAlways                AuthModeRequirement = "always"
	AuthModeRequirementNever                 AuthModeRequirement = "never"
	AuthModeRequirementExceptLoopbackExecute AuthModeRequirement = "except_loopback_execute_origin"
)

// AuthModeSpec declares one allowed auth path for a provider.
type AuthModeSpec struct {
	Mode        AuthMode
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
	ProviderSpecAzure            ProviderID = "azure"
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
	DefaultAuthHeader       string
	VisibleInOperatorUI     bool
	ProviderProtocols       []ProviderProtocolSpec
	// AllowedAuthModes lists the auth paths this provider declares.
	AllowedAuthModes     []AuthModeSpec
	DeclaredCapabilities []Capability
}

type ProviderProtocolSpec struct {
	Name            string
	Kind            protocolkind.ProtocolKind
	Frame           string
	RequestFeatures []RequestFeature
}

var (
	openAICompatibleAuthHeaders = []string{
		"Authorization",
		"X-API-Key",
		"api-key",
	}
	providerRequestFeaturesResponses = []RequestFeature{
		RequestFeatureFunctionTools,
		RequestFeatureToolChoiceNone,
		RequestFeatureToolChoiceRequired,
		RequestFeatureToolChoiceSpecific,
		RequestFeatureToolBatchAtMostOne,
		RequestFeatureMaxOutputTokens,
		RequestFeatureTemperature,
		RequestFeatureTopP,
		RequestFeatureJSONSchemaOutput,
		RequestFeatureUsageReasoningTokens,
	}
	providerRequestFeaturesChatCompletions = []RequestFeature{
		RequestFeatureFunctionTools,
		RequestFeatureToolChoiceNone,
		RequestFeatureToolChoiceRequired,
		RequestFeatureToolChoiceSpecific,
		RequestFeatureToolBatchAtMostOne,
		RequestFeatureMaxOutputTokens,
		RequestFeatureTemperature,
		RequestFeatureTopP,
		RequestFeatureStopSequences,
		RequestFeatureJSONSchemaOutput,
		RequestFeatureUsageReasoningTokens,
	}
	providerRequestFeaturesMessages = []RequestFeature{
		RequestFeatureFunctionTools,
		RequestFeatureToolChoiceNone,
		RequestFeatureToolChoiceRequired,
		RequestFeatureToolChoiceSpecific,
		RequestFeatureToolBatchAtMostOne,
		RequestFeatureMaxOutputTokens,
		RequestFeatureTemperature,
		RequestFeatureTopP,
		RequestFeatureStopSequences,
	}
	providerRequestFeaturesCompletions = []RequestFeature{
		RequestFeatureMaxOutputTokens,
		RequestFeatureTemperature,
		RequestFeatureTopP,
		RequestFeatureStopSequences,
	}
	providerProtocolsOpenAIFamily = []ProviderProtocolSpec{
		{Name: "responses", Kind: protocolkind.Responses, Frame: FrameHTTPJSONBody, RequestFeatures: slices.Clone(providerRequestFeaturesResponses)},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent, RequestFeatures: slices.Clone(providerRequestFeaturesResponses)},
		{Name: "chat_completions", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody, RequestFeatures: slices.Clone(providerRequestFeaturesChatCompletions)},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent, RequestFeatures: slices.Clone(providerRequestFeaturesChatCompletions)},
		{Name: "completions", Kind: protocolkind.Completions, Frame: FrameHTTPJSONBody, RequestFeatures: slices.Clone(providerRequestFeaturesCompletions)},
		{Name: "completions_stream", Kind: protocolkind.Completions, Frame: FrameSSEEvent, RequestFeatures: slices.Clone(providerRequestFeaturesCompletions)},
	}
	providerProtocolsChatGPT = []ProviderProtocolSpec{
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent, RequestFeatures: slices.Clone(providerRequestFeaturesResponses)},
	}
	providerProtocolsAnthropic = []ProviderProtocolSpec{
		{Name: "messages", Kind: protocolkind.Messages, Frame: FrameHTTPJSONBody, RequestFeatures: slices.Clone(providerRequestFeaturesMessages)},
		{Name: "messages_stream", Kind: protocolkind.Messages, Frame: FrameSSEEvent, RequestFeatures: slices.Clone(providerRequestFeaturesMessages)},
	}
	providerProtocolsBedrock = []ProviderProtocolSpec{
		{Name: "responses", Kind: protocolkind.Responses, Frame: FrameHTTPJSONBody, RequestFeatures: slices.Clone(providerRequestFeaturesResponses)},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent, RequestFeatures: slices.Clone(providerRequestFeaturesResponses)},
		{Name: "chat_completions", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody, RequestFeatures: slices.Clone(providerRequestFeaturesChatCompletions)},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent, RequestFeatures: slices.Clone(providerRequestFeaturesChatCompletions)},
		{Name: "messages", Kind: protocolkind.Messages, Frame: FrameHTTPJSONBody, RequestFeatures: slices.Clone(providerRequestFeaturesMessages)},
		{Name: "messages_stream", Kind: protocolkind.Messages, Frame: FrameSSEEvent, RequestFeatures: slices.Clone(providerRequestFeaturesMessages)},
	}
	providerProtocolsAzure = []ProviderProtocolSpec{
		{Name: "responses", Kind: protocolkind.Responses, Frame: FrameHTTPJSONBody, RequestFeatures: slices.Clone(providerRequestFeaturesResponses)},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent, RequestFeatures: slices.Clone(providerRequestFeaturesResponses)},
		{Name: "chat_completions", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody, RequestFeatures: slices.Clone(providerRequestFeaturesChatCompletions)},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent, RequestFeatures: slices.Clone(providerRequestFeaturesChatCompletions)},
		{Name: "completions", Kind: protocolkind.Completions, Frame: FrameHTTPJSONBody, RequestFeatures: slices.Clone(providerRequestFeaturesCompletions)},
		{Name: "completions_stream", Kind: protocolkind.Completions, Frame: FrameSSEEvent, RequestFeatures: slices.Clone(providerRequestFeaturesCompletions)},
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
				{Mode: "", Kind: AuthNone, Requirement: AuthModeRequirementNever},
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
				{Mode: AuthModeEnv, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{Mode: AuthModeFile, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{Mode: AuthModeKeychain, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
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
				{Mode: AuthModeChatGPTLogin, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways, Interactive: true},
				{Mode: AuthModeChatGPTDeviceAuth, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways, Interactive: true},
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
				{Mode: AuthModeEnv, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{Mode: AuthModeFile, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{Mode: AuthModeKeychain, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
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
				{Mode: AuthModeEnv, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{Mode: AuthModeFile, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{Mode: AuthModeKeychain, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
		{
			ProviderID:              ProviderSpecBedrock,
			ProviderDisplayName:     "AWS Bedrock",
			SetupHint:               string(ProviderSpecBedrock) + "   Bedrock Mantle endpoint URL",
			DefaultBaseURL:          "",
			DefaultCredentialEnvVar: "AWS_BEARER_TOKEN_BEDROCK",
			VisibleInOperatorUI:     true,
			ProviderProtocols:       slices.Clone(providerProtocolsBedrock),
			AllowedAuthModes: []AuthModeSpec{
				{Mode: AuthModeAWSProfile, Kind: AuthNone, Requirement: AuthModeRequirementNever},
				{Mode: AuthModeAWSEnvSession, Kind: AuthNone, Requirement: AuthModeRequirementNever},
				{Mode: AuthModeEnv, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
		{
			ProviderID:              ProviderSpecAzure,
			ProviderDisplayName:     "Azure OpenAI",
			SetupHint:               string(ProviderSpecAzure) + "   Azure OpenAI v1 endpoint (https://resource.openai.azure.com/openai/v1)",
			DefaultBaseURL:          "",
			DefaultCredentialEnvVar: "AZURE_OPENAI_API_KEY",
			VisibleInOperatorUI:     true,
			ProviderProtocols:       slices.Clone(providerProtocolsAzure),
			AllowedAuthModes: []AuthModeSpec{
				{Mode: AuthModeEnv, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{Mode: AuthModeFile, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{Mode: AuthModeKeychain, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
		{
			ProviderID:          ProviderSpecOpenAICompatible,
			ProviderDisplayName: "OpenAI Compatible",
			SetupHint:           string(ProviderSpecOpenAICompatible) + "   OpenAI-style URL (https://host/v1)",
			DefaultBaseURL:      "",
			DefaultAuthHeader:   "Authorization",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsOpenAIFamily),
			AllowedAuthModes: []AuthModeSpec{
				{Mode: "", Kind: AuthNone, Requirement: AuthModeRequirementExceptLoopbackExecute},
				{Mode: AuthModeEnv, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{Mode: AuthModeFile, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
				{Mode: AuthModeKeychain, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways},
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

func SupportedAuthModesForSpec(spec string) []AuthMode {
	modes := AllowedAuthModesForSpec(spec)
	out := make([]AuthMode, 0, len(modes))
	for _, mode := range modes {
		threadingMode := mode.Mode
		if strings.TrimSpace(string(threadingMode)) == "" { // swobu:io-string source=domain
			continue
		}
		out = append(out, threadingMode)
	}
	return slices.Compact(out)
}

func SupportsAuthMode(spec string, mode AuthMode) bool {
	for _, supported := range SupportedAuthModesForSpec(spec) {
		if supported == mode {
			return true
		}
	}
	return false
}

func IsInteractiveAuthMode(mode AuthMode) bool {
	switch mode {
	case AuthModeChatGPTLogin, AuthModeChatGPTDeviceAuth:
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
