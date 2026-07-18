package profile

import (
	"slices"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

var (
	openAICompatibleAuthHeaders = []string{
		"Authorization",
		"x-api-key",
		"api-key",
	}
	providerProtocolsOpenAIFamily = []ProviderProtocolSpec{
		{Name: "responses", Kind: protocolkind.Responses, Frame: FrameHTTPJSONBody},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
		{Name: "chat_completions", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
	}
	providerProtocolsOllama = []ProviderProtocolSpec{
		{Name: "chat_completions", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
	}
	providerProtocolsChatGPT = []ProviderProtocolSpec{
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
	}
	providerProtocolsAnthropic = []ProviderProtocolSpec{
		{Name: "messages", Kind: protocolkind.Messages, Frame: FrameHTTPJSONBody},
		{Name: "messages_stream", Kind: protocolkind.Messages, Frame: FrameSSEEvent},
	}
	providerProtocolsBedrock = []ProviderProtocolSpec{
		{Name: "responses", Kind: protocolkind.Responses, Frame: FrameHTTPJSONBody},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
		{Name: "chat_completions", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
		{Name: "messages", Kind: protocolkind.Messages, Frame: FrameHTTPJSONBody},
		{Name: "messages_stream", Kind: protocolkind.Messages, Frame: FrameSSEEvent},
	}
	providerProtocolsAzure = []ProviderProtocolSpec{
		{Name: "responses", Kind: protocolkind.Responses, Frame: FrameHTTPJSONBody},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
		{Name: "chat_completions", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
		{Name: "messages", Kind: protocolkind.Messages, Frame: FrameHTTPJSONBody},
		{Name: "messages_stream", Kind: protocolkind.Messages, Frame: FrameSSEEvent},
	}
	// providerProtocolsCustomEndpoint carries the OpenAI-family protocols plus
	// the Anthropic Messages protocols, because a Custom Endpoint can front an
	// Anthropic-style backend. Native OpenAI/OpenRouter keep the narrower family
	// list; only Custom Endpoint declares this cross-family surface.
	providerProtocolsCustomEndpoint = []ProviderProtocolSpec{
		{Name: "responses", Kind: protocolkind.Responses, Frame: FrameHTTPJSONBody},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
		{Name: "chat_completions", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
		{Name: "messages", Kind: protocolkind.Messages, Frame: FrameHTTPJSONBody},
		{Name: "messages_stream", Kind: protocolkind.Messages, Frame: FrameSSEEvent},
	}
)

func catalog() []Profile {
	return []Profile{
		{
			ProviderID:          ProviderSpecOllama,
			ProviderDisplayName: "Ollama",
			SetupHint:           "local model catalog",
			SetupKeywords:       []string{"model", "protocol"},
			Endpoint: EndpointSpec{
				Kind:       EndpointDefaultHTTPBaseURL,
				DefaultURL: "http://127.0.0.1:11434/v1",
				Label:      "base URL",
			},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsOllama),
			AllowedAuthModes: []AuthModeSpec{
				{Mode: "", Kind: AuthNone, Requirement: AuthModeRequirementNever},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
		{
			ProviderID:          ProviderSpecOpenAI,
			ProviderDisplayName: "OpenAI",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "protocol"},
			Endpoint: EndpointSpec{
				Kind:       EndpointDefaultHTTPBaseURL,
				DefaultURL: "https://api.openai.com/v1",
				Label:      "base URL",
			},
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
			SetupHint:           "browser login",
			SetupKeywords:       []string{"sign in", "model", "protocol"},
			Endpoint: EndpointSpec{
				Kind:       EndpointDefaultHTTPBaseURL,
				DefaultURL: "https://api.openai.com/v1",
				Label:      "base URL",
			},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsChatGPT),
			AllowedAuthModes: []AuthModeSpec{
				{Mode: AuthModeChatGPTLogin, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways, Interactive: true},
				{Mode: AuthModeChatGPTDeviceAuth, Kind: AuthCredentialRef, Requirement: AuthModeRequirementAlways, Interactive: true},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
		{
			ProviderID:          ProviderSpecAnthropic,
			ProviderDisplayName: "Anthropic",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "protocol"},
			Endpoint: EndpointSpec{
				Kind:       EndpointDefaultHTTPBaseURL,
				DefaultURL: "https://api.anthropic.com/v1",
				Label:      "base URL",
			},
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
			ProviderID:          ProviderSpecOpenRouter,
			ProviderDisplayName: "OpenRouter",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "protocol"},
			Endpoint: EndpointSpec{
				Kind:       EndpointDefaultHTTPBaseURL,
				DefaultURL: "https://openrouter.ai/api/v1",
				Label:      "base URL",
			},
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
			ProviderID:          ProviderSpecBedrock,
			ProviderDisplayName: "AWS Bedrock",
			SetupHint:           "region / auth",
			SetupKeywords:       []string{"region", "Bedrock API key", "AWS credentials", "AWS profile", "AWS env", "AWS_BEARER_TOKEN_BEDROCK", "access keys", "model", "protocol"},
			Endpoint: EndpointSpec{
				Kind:  EndpointRequiredHTTPBaseURL,
				Label: "region",
			},
			DefaultCredentialEnvVar: "AWS_BEARER_TOKEN_BEDROCK",
			VisibleInOperatorUI:     true,
			ProviderProtocols:       slices.Clone(providerProtocolsBedrock),
			AllowedAuthModes: []AuthModeSpec{
				{
					Mode:        AuthModeEnv,
					Kind:        AuthCredentialRef,
					Requirement: AuthModeRequirementAlways,
					Label:       "Bedrock API key",
					Keywords:    []string{"bedrock", "api key", "aws_bearer_token_bedrock", "openai_api_key", "env", "file", "keychain"},
				},
				{
					Mode:        AuthModeAWSEnvSession,
					Kind:        AuthNone,
					Requirement: AuthModeRequirementNever,
					Label:       "AWS env",
					Keywords:    []string{"aws", "credentials", "access keys", "aws_access_key_id", "aws_secret_access_key", "aws_session_token", "sigv4"},
				},
				{
					Mode:        AuthModeAWSProfile,
					Kind:        AuthNone,
					Requirement: AuthModeRequirementNever,
					Label:       "AWS profile",
					Keywords:    []string{"aws", "profile", "default", "shared config"},
				},
			},
			DeclaredCapabilities: []Capability{CapabilityModelCatalog, CapabilityStreaming},
		},
		{
			ProviderID:          ProviderSpecAzure,
			ProviderDisplayName: "Azure AI",
			SetupHint:           "endpoint",
			SetupKeywords:       []string{"endpoint", "credential", "deployment", "protocol"},
			Endpoint: EndpointSpec{
				Kind:  EndpointAzureResourceLocator,
				Label: "project",
			},
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
			ProviderDisplayName: "Custom Endpoint",
			SetupHint:           "OpenAI-style URL",
			SetupKeywords:       []string{"backend URL", "credential", "credential header", "model", "protocol"},
			Endpoint: EndpointSpec{
				Kind:  EndpointRequiredHTTPBaseURL,
				Label: "backend URL",
			},
			DefaultAuthHeader:   "Authorization",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsCustomEndpoint),
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
