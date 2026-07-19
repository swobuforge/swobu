package profile

import (
	"slices"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

var (
	customAuthHeaders = []string{
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
	// providerProtocolsCustomEndpoint lists every protocol an operator may
	// explicitly select for a user-supplied HTTP endpoint.
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
			Locator: LocatorSpec{
				Kind:    LocatorBaseURL,
				Default: "http://127.0.0.1:11434/v1",
				Label:   "base URL",
			},
			Credential:          CredentialSpec{Requirement: CredentialUnsupported},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsOllama),
		},
		{
			ProviderID:          ProviderSpecOpenAI,
			ProviderDisplayName: "OpenAI",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.openai.com/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, SuggestedEnvVar: "OPENAI_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsOpenAIFamily),
		},
		{
			ProviderID:          ProviderSpecChatGPT,
			ProviderDisplayName: "ChatGPT",
			SetupHint:           "browser login",
			SetupKeywords:       []string{"sign in", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.openai.com/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialUnsupported},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsChatGPT),
		},
		{
			ProviderID:          ProviderSpecAnthropic,
			ProviderDisplayName: "Anthropic",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.anthropic.com/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, SuggestedEnvVar: "ANTHROPIC_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAnthropic),
		},
		{
			ProviderID:          ProviderSpecOpenRouter,
			ProviderDisplayName: "OpenRouter",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://openrouter.ai/api/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, SuggestedEnvVar: "OPENROUTER_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsOpenAIFamily),
		},
		{
			ProviderID:          ProviderSpecBedrock,
			ProviderDisplayName: "AWS Bedrock",
			SetupHint:           "region / AWS identity",
			SetupKeywords:       []string{"region", "Bedrock API key", "AWS credentials", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:  LocatorAWSRegion,
				Label: "region",
			},
			Credential:          CredentialSpec{Requirement: CredentialOptional, SuggestedEnvVar: "AWS_BEARER_TOKEN_BEDROCK"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsBedrock),
		},
		{
			ProviderID:          ProviderSpecAzure,
			ProviderDisplayName: "Azure AI",
			SetupHint:           "endpoint",
			SetupKeywords:       []string{"endpoint", "credential", "deployment", "protocol"},
			Locator: LocatorSpec{
				Kind:  LocatorAzureProject,
				Label: "project",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, SuggestedEnvVar: "AZURE_OPENAI_API_KEY"},
			CatalogItemLabel:    "deployment",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAzure),
		},
		{
			ProviderID:          ProviderSpecCustom,
			ProviderDisplayName: "Custom Endpoint",
			SetupHint:           "backend URL",
			SetupKeywords:       []string{"backend URL", "credential", "credential header", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:  LocatorBaseURL,
				Label: "backend URL",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequiredOutsideLoopback},
			DefaultAuthHeader:   "Authorization",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsCustomEndpoint),
		},
	}
}
