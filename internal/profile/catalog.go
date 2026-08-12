package profile

import (
	"fmt"
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
	providerProtocolsKimi = []ProviderProtocolSpec{
		{Name: routing.KimiProviderProtocol, Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
	}
	providerProtocolsFriendli = []ProviderProtocolSpec{
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
		{Name: "messages_stream", Kind: protocolkind.Messages, Frame: FrameSSEEvent},
	}
	providerProtocolsTogether = []ProviderProtocolSpec{
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
	}
	providerProtocolsDeepInfra = []ProviderProtocolSpec{
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
	}
	providerProtocolsScaleway = []ProviderProtocolSpec{
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
	}
	providerProtocolsGroq = []ProviderProtocolSpec{
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
	}
	providerProtocolsSambaNova = []ProviderProtocolSpec{
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
		{Name: "messages_stream", Kind: protocolkind.Messages, Frame: FrameSSEEvent},
		{Name: "chat_completions", Kind: protocolkind.ChatCompletions, Frame: FrameHTTPJSONBody},
		{Name: "responses", Kind: protocolkind.Responses, Frame: FrameHTTPJSONBody},
		{Name: "messages", Kind: protocolkind.Messages, Frame: FrameHTTPJSONBody},
	}
	providerProtocolsStepFun = []ProviderProtocolSpec{
		{Name: "chat_completions_stream", Kind: protocolkind.ChatCompletions, Frame: FrameSSEEvent},
		{Name: "messages_stream", Kind: protocolkind.Messages, Frame: FrameSSEEvent},
		{Name: "responses_stream", Kind: protocolkind.Responses, Frame: FrameSSEEvent},
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
	profiles := []Profile{
		{
			ProviderID:          ProviderSpecOllama,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "Ollama",
			SetupHint:           "local model catalog",
			SetupKeywords:       []string{"model", "protocol"},
			Locator: LocatorSpec{
				Kind:    LocatorBaseURL,
				Default: "http://127.0.0.1:11434/v1",
				Label:   "base URL",
			},
			Credential:          CredentialSpec{Requirement: CredentialUnsupported, Authoring: CredentialAuthoringNone},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAllStandard),
		},
		{
			ProviderID:          ProviderSpecLMStudio,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "LM Studio",
			SetupHint:           "local model server",
			SetupKeywords:       []string{"local", "model", "Responses", "Chat Completions", "Messages", "Codex", "Claude Code"},
			Locator: LocatorSpec{
				Kind:    LocatorBaseURL,
				Default: "http://127.0.0.1:1234/v1",
				Label:   "base URL",
			},
			Credential:          CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "LM_API_TOKEN"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAllStandard),
		},
		{
			ProviderID:          ProviderSpecVLLM,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "vLLM",
			SetupHint:           "inference server",
			SetupKeywords:       []string{"inference", "server", "Responses", "Chat Completions", "Messages", "Codex", "Claude Code"},
			Locator: LocatorSpec{
				Kind:    LocatorBaseURL,
				Default: "http://127.0.0.1:8000/v1",
				Label:   "base URL",
			},
			Credential:          CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "VLLM_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAllStandard),
		},
		{
			ProviderID:          ProviderSpecOpenAI,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "OpenAI",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.openai.com/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "OPENAI_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsOpenAIFamily),
		},
		{
			ProviderID:          ProviderSpecChatGPT,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "ChatGPT",
			SetupHint:           "browser login",
			SetupKeywords:       []string{"sign in", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.openai.com/v1",
			},
			// Browser login writes the durable credential reference; it is not a
			// generic API-key authoring flow.
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringInteractive},
			VisibleInOperatorUI: true,
			ProtocolAuthoring:   ProtocolDerived,
			ProviderProtocols:   slices.Clone(providerProtocolsChatGPT),
		},
		{
			ProviderID:          ProviderSpecAnthropic,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "Anthropic",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.anthropic.com/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "ANTHROPIC_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAnthropic),
		},
		{
			ProviderID:          ProviderSpecDeepSeek,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "DeepSeek",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "V4", "Pro", "Flash", "thinking", "web search"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.deepseek.com/anthropic/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "DEEPSEEK_API_KEY"},
			VisibleInOperatorUI: true,
			ProtocolAuthoring:   ProtocolDerived,
			ProviderProtocols:   slices.Clone(providerProtocolsDeepSeek),
		},
		{
			ProviderID:          ProviderSpecKimi,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "Kimi",
			SetupHint:           "API key",
			SetupKeywords:       []string{"Moonshot", "K3", "K2.7", "thinking", "coding", "credential", "model"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.moonshot.ai/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "MOONSHOT_API_KEY"},
			VisibleInOperatorUI: true,
			ProtocolAuthoring:   ProtocolDerived,
			ProviderProtocols:   slices.Clone(providerProtocolsKimi),
		},
		{
			ProviderID:          ProviderSpecFriendli,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeManual,
			ProviderDisplayName: "FriendliAI",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"Friendli", "serverless", "dedicated", "container", "base URL", "credential", "model", "endpoint", "protocol"},
			Locator: LocatorSpec{
				Kind:    LocatorBaseURL,
				Label:   "base URL",
				Default: "https://api.friendli.ai/serverless/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "FRIENDLI_TOKEN"},
			DefaultAuthHeader:   "Authorization",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsFriendli),
		},
		{
			ProviderID:          ProviderSpecTogether,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "Together AI",
			SetupHint:           "API key",
			SetupKeywords:       []string{"Together", "credential", "model", "dedicated", "reasoning"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://api.together.ai/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "TOGETHER_API_KEY"},
			VisibleInOperatorUI: true,
			ProtocolAuthoring:   ProtocolDerived,
			ProviderProtocols:   slices.Clone(providerProtocolsTogether),
		},
		{
			ProviderID:          ProviderSpecDeepInfra,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "DeepInfra",
			SetupHint:           "API token",
			SetupKeywords:       []string{"DeepInfra", "credential", "model", "private deployment", "deploy_id", "Chat Completions"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://api.deepinfra.com/v1/openai"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "DEEPINFRA_TOKEN"},
			VisibleInOperatorUI: true,
			ProtocolAuthoring:   ProtocolDerived,
			ProviderProtocols:   slices.Clone(providerProtocolsDeepInfra),
		},
		{
			ProviderID:          ProviderSpecScaleway,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "Scaleway",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"Scaleway", "Generative APIs", "credential", "model", "dedicated", "base URL", "Chat Completions", "Responses"},
			Locator:             LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.scaleway.ai/v1"},
			Credential:          CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "SCW_SECRET_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsScaleway),
		},
		{
			ProviderID:          ProviderSpecGroq,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "Groq",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"Groq", "credential", "model", "base URL", "Responses", "Chat Completions"},
			Locator:             LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.groq.com/openai/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "GROQ_API_KEY"},
			DefaultAuthHeader:   "Authorization",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsGroq),
		},
		{
			ProviderID:          ProviderSpecFireworks,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeManual,
			ProviderDisplayName: "Fireworks AI",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"Fireworks", "credential", "serverless", "deployment", "router", "base URL", "Responses", "Messages", "MCP"},
			Locator:             LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.fireworks.ai/inference/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "FIREWORKS_API_KEY"},
			DefaultAuthHeader:   "Authorization",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAllStandard),
		},
		{
			ProviderID:          ProviderSpecSambaNova,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "SambaNova",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"SambaNova", "SambaCloud", "SambaStack", "credential", "model", "base URL", "Chat Completions", "Responses", "Messages"},
			Locator:             LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.sambanova.ai/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "SAMBANOVA_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsSambaNova),
		},
		{
			ProviderID:          ProviderSpecStepFun,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "StepFun",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"StepFun", "Step Plan", "credential", "model", "base URL", "Chat Completions", "Messages", "Responses"},
			Locator:             LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.stepfun.com/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "STEP_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsStepFun),
		},
		{
			ProviderID:          ProviderSpecNebius,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "Nebius Token Factory",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"Nebius", "Token Factory", "credential", "model", "dedicated", "base URL", "Responses", "Chat Completions"},
			Locator: LocatorSpec{
				Kind:    LocatorBaseURL,
				Label:   "base URL",
				Default: "https://api.tokenfactory.nebius.com/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "NEBIUS_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsOpenAIFamily),
		},
		{
			ProviderID:          ProviderSpecGMI,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "GMI Cloud",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"GMI", "Cloud", "credential", "model", "base URL", "Responses", "Chat Completions", "Messages"},
			Locator:             LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.gmi-serving.com/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "GMI_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAllStandard),
		},
		{
			ProviderID:          ProviderSpecOpenRouter,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "OpenRouter",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://openrouter.ai/api/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "OPENROUTER_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsOpenAIFamily),
		},
		{
			ProviderID:          ProviderSpecZAI,
			ModelCatalog:        ModelCatalogModeManual,
			ProviderDisplayName: "Z.AI",
			SetupHint:           "access / API key",
			SetupKeywords:       []string{"access", "General API", "Coding Plan", "credential", "model", "GLM"},
			Locator: LocatorSpec{
				Kind: LocatorFixed,
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "ZAI_API_KEY"},
			ConnectionShape:     routing.ConnectionShapeZAI,
			VisibleInOperatorUI: true,
			ProtocolAuthoring:   ProtocolDerived,
			ProviderProtocols:   slices.Clone(providerProtocolsZAI),
		},
		{
			ProviderID:          ProviderSpecBedrock,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "AWS Bedrock",
			SetupHint:           "region / AWS identity",
			SetupKeywords:       []string{"region", "Bedrock API key", "AWS credentials", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:  LocatorAWSRegion,
				Label: "region",
			},
			Credential:          CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringAmbientOrReference, SuggestedEnvVar: "AWS_BEARER_TOKEN_BEDROCK"},
			ConnectionShape:     routing.ConnectionShapeBedrock,
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsBedrock),
		},
		{
			ProviderID:          ProviderSpecAzure,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "Azure AI",
			SetupHint:           "endpoint",
			SetupKeywords:       []string{"endpoint", "credential", "deployment", "protocol"},
			Locator: LocatorSpec{
				Kind:  LocatorAzureProject,
				Label: "project",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "AZURE_OPENAI_API_KEY"},
			CatalogItemLabel:    "deployment",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAzure),
		},
		{
			ProviderID:          ProviderSpecCustom,
			ModelCatalog:        ModelCatalogModeEnumerable,
			ProviderDisplayName: "Custom Endpoint",
			SetupHint:           "backend URL",
			SetupKeywords:       []string{"backend URL", "credential", "credential header", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:  LocatorBaseURL,
				Label: "backend URL",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequiredOutsideLoopback, Authoring: CredentialAuthoringReference},
			ConnectionShape:     routing.ConnectionShapeCustom,
			DefaultAuthHeader:   "Authorization",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAllStandard),
		},
	}
	for _, provider := range profiles {
		if err := ValidateCatalogProfile(provider); err != nil {
			panic(fmt.Sprintf("invalid provider catalog: %v", err))
		}
	}
	return profiles
}
