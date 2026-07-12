package profile

import (
	"slices"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

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
