package profile

import (
	"slices"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
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
	// providerProtocolsAllStandard is the preference-ordered set for providers
	// that implement all three standard inference families in both delivery modes.
	providerProtocolsAllStandard = []ProviderProtocolSpec{
		{Name: "responses", Kind: protocolkind.Responses, Frame: FrameHTTPJSONBody},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
		{Name: "chat_completions", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
		{Name: "messages", Kind: protocolkind.Messages, Frame: FrameHTTPJSONBody},
		{Name: "messages_stream", Kind: protocolkind.Messages, Frame: FrameSSEEvent},
	}
	providerProtocolsZAI = []ProviderProtocolSpec{
		{Name: routing.ZAIProviderProtocol, Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
	}
	providerProtocolsDeepSeek = []ProviderProtocolSpec{
		{Name: routing.DeepSeekProviderProtocol, Kind: protocolkind.Messages, Frame: FrameSSEEvent},
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
			ProviderProtocols:   slices.Clone(providerProtocolsAllStandard),
		},
		{
			ProviderID:          ProviderSpecLMStudio,
			ProviderDisplayName: "LM Studio",
			SetupHint:           "local model server",
			SetupKeywords:       []string{"local", "model", "Responses", "Chat Completions", "Messages", "Codex", "Claude Code"},
			Locator: LocatorSpec{
				Kind:    LocatorBaseURL,
				Default: "http://127.0.0.1:1234/v1",
				Label:   "base URL",
			},
			Credential:          CredentialSpec{Requirement: CredentialOptional, SuggestedEnvVar: "LM_API_TOKEN"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAllStandard),
		},
		{
			ProviderID:          ProviderSpecVLLM,
			ProviderDisplayName: "vLLM",
			SetupHint:           "inference server",
			SetupKeywords:       []string{"inference", "server", "Responses", "Chat Completions", "Messages", "Codex", "Claude Code"},
			Locator: LocatorSpec{
				Kind:    LocatorBaseURL,
				Default: "http://127.0.0.1:8000/v1",
				Label:   "base URL",
			},
			Credential:          CredentialSpec{Requirement: CredentialOptional, SuggestedEnvVar: "VLLM_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAllStandard),
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
			ProtocolAuthoring:   ProtocolDerived,
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
			ProviderID:          ProviderSpecDeepSeek,
			ProviderDisplayName: "DeepSeek",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "V4", "Pro", "Flash", "thinking", "web search"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.deepseek.com/anthropic/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, SuggestedEnvVar: "DEEPSEEK_API_KEY"},
			VisibleInOperatorUI: true,
			ProtocolAuthoring:   ProtocolDerived,
			ProviderProtocols:   slices.Clone(providerProtocolsDeepSeek),
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
			ProviderID:          ProviderSpecZAI,
			ProviderDisplayName: "Z.AI",
			SetupHint:           "access / API key",
			SetupKeywords:       []string{"access", "General API", "Coding Plan", "credential", "model", "GLM"},
			Locator: LocatorSpec{
				Kind: LocatorFixed,
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, SuggestedEnvVar: "ZAI_API_KEY"},
			VisibleInOperatorUI: true,
			ProtocolAuthoring:   ProtocolDerived,
			ProviderProtocols:   slices.Clone(providerProtocolsZAI),
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
			ProviderProtocols:   slices.Clone(providerProtocolsAllStandard),
		},
	}
}
