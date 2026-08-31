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
		bufferedProtocol("responses", protocolkind.Responses),
		streamingProtocol("responses_stream", protocolkind.Responses),
		bufferedProtocol("chat_completions", protocolkind.ChatCompletions),
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	// providerProtocolsAllStandard is the preference-ordered set for providers
	// that implement all three standard inference families in both delivery
	// modes. Each entry is a selectable concrete provider contract.
	providerProtocolsAllStandard = []ProviderProtocolSpec{
		bufferedProtocol("responses", protocolkind.Responses),
		streamingProtocol("responses_stream", protocolkind.Responses),
		bufferedProtocol("chat_completions", protocolkind.ChatCompletions),
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		bufferedProtocol("messages", protocolkind.Messages),
		streamingProtocol("messages_stream", protocolkind.Messages),
	}
	providerProtocolsZAI = []ProviderProtocolSpec{
		streamingProtocol(routing.ZAIProviderProtocol, protocolkind.ChatCompletions),
	}
	providerProtocolsDeepSeek = []ProviderProtocolSpec{
		streamingProtocol(routing.DeepSeekProviderProtocol, protocolkind.Messages),
	}
	providerProtocolsKimi = []ProviderProtocolSpec{
		streamingProtocol(routing.KimiProviderProtocol, protocolkind.ChatCompletions),
	}
	providerProtocolsMistral = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsCerebras = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsWorkersAI = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		bufferedProtocol("chat_completions", protocolkind.ChatCompletions),
		streamingProtocol("responses_stream", protocolkind.Responses),
		bufferedProtocol("responses", protocolkind.Responses),
	}
	providerProtocolsLLM7 = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsNVIDIA = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsOVHCloud = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsModelScope = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsNous = []ProviderProtocolSpec{
		bufferedProtocol("chat_completions", protocolkind.ChatCompletions),
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsModelRouted = []ProviderProtocolSpec{
		bufferedProtocol("responses", protocolkind.Responses),
		streamingProtocol("responses_stream", protocolkind.Responses),
		bufferedProtocol("chat_completions", protocolkind.ChatCompletions),
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		bufferedProtocol("messages", protocolkind.Messages),
		streamingProtocol("messages_stream", protocolkind.Messages),
	}
	providerProtocolsCommandCode = []ProviderProtocolSpec{
		bufferedProtocol("chat_completions", protocolkind.ChatCompletions),
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		bufferedProtocol("messages", protocolkind.Messages),
		streamingProtocol("messages_stream", protocolkind.Messages),
	}
	providerProtocolsVenice = []ProviderProtocolSpec{
		bufferedProtocol("chat_completions", protocolkind.ChatCompletions),
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsRunPod = []ProviderProtocolSpec{
		bufferedProtocol("responses", protocolkind.Responses),
		streamingProtocol("responses_stream", protocolkind.Responses),
		bufferedProtocol("chat_completions", protocolkind.ChatCompletions),
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		bufferedProtocol("messages", protocolkind.Messages),
		streamingProtocol("messages_stream", protocolkind.Messages),
	}
	providerProtocolsFriendli = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		streamingProtocol("responses_stream", protocolkind.Responses),
		streamingProtocol("messages_stream", protocolkind.Messages),
	}
	providerProtocolsTogether = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsDeepInfra = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsScaleway = []ProviderProtocolSpec{
		streamingProtocol("responses_stream", protocolkind.Responses),
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsGroq = []ProviderProtocolSpec{
		streamingProtocol("responses_stream", protocolkind.Responses),
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsSambaNova = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		streamingProtocol("responses_stream", protocolkind.Responses),
		streamingProtocol("messages_stream", protocolkind.Messages),
		bufferedProtocol("chat_completions", protocolkind.ChatCompletions),
		bufferedProtocol("responses", protocolkind.Responses),
		bufferedProtocol("messages", protocolkind.Messages),
	}
	providerProtocolsStepFun = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		streamingProtocol("messages_stream", protocolkind.Messages),
		streamingProtocol("responses_stream", protocolkind.Responses),
	}
	providerProtocolsChatGPT = []ProviderProtocolSpec{
		streamingProtocol("responses_stream", protocolkind.Responses),
	}
	providerProtocolsGemini = []ProviderProtocolSpec{
		streamingProtocol("interactions_stream", protocolkind.Interactions),
	}
	providerProtocolsAnthropic = []ProviderProtocolSpec{
		bufferedProtocol("messages", protocolkind.Messages),
		streamingProtocol("messages_stream", protocolkind.Messages),
	}
	providerProtocolsBedrock = []ProviderProtocolSpec{
		bufferedProtocol("responses", protocolkind.Responses),
		streamingProtocol("responses_stream", protocolkind.Responses),
		bufferedProtocol("chat_completions", protocolkind.ChatCompletions),
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		bufferedProtocol("messages", protocolkind.Messages),
		streamingProtocol("messages_stream", protocolkind.Messages),
	}
	providerProtocolsAzure = []ProviderProtocolSpec{
		bufferedProtocol("responses", protocolkind.Responses),
		streamingProtocol("responses_stream", protocolkind.Responses),
		bufferedProtocol("chat_completions", protocolkind.ChatCompletions),
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		bufferedProtocol("messages", protocolkind.Messages),
		streamingProtocol("messages_stream", protocolkind.Messages),
	}
)

func catalog() []Profile {
	profiles := []Profile{
		{
			ProviderID:          ProviderSpecOpenCodeZen,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "OpenCode Zen",
			SetupHint:           "API key",
			SetupKeywords:       []string{"OpenCode", "Zen", "credential", "model", "protocol"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://opencode.ai/zen/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "OPENCODE_ZEN_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsModelRouted),
		},
		{
			ProviderID:          ProviderSpecNous,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Nous Portal",
			SetupHint:           "API key",
			SetupKeywords:       []string{"Nous", "Hermes", "credential", "model", "Chat Completions"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://inference-api.nousresearch.com/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "NOUS_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsNous),
		},
		{
			ProviderID:          ProviderSpecCommandCode,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Command Code",
			SetupHint:           "API key",
			SetupKeywords:       []string{"Command Code", "credential", "model", "protocol"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://api.commandcode.ai/provider/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "COMMANDCODE_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsCommandCode),
		},
		{
			ProviderID:          ProviderSpecVenice,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Venice AI",
			SetupHint:           "API key",
			SetupKeywords:       []string{"Venice", "credential", "model", "web search", "reasoning"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://api.venice.ai/api/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "VENICE_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsVenice),
		},
		{
			ProviderID:          ProviderSpecOllama,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ProviderID:          ProviderSpecMeta,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Meta Model API",
			SetupHint:           "API key",
			SetupKeywords:       []string{"Meta", "Muse", "Muse Spark", "Model API", "MODEL_API_KEY"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.meta.ai/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "MODEL_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols: []ProviderProtocolSpec{
				streamingProtocol("responses_stream", protocolkind.Responses),
			},
		},
		{
			ProviderID:          ProviderSpecChatGPT,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ProviderProtocols:   slices.Clone(providerProtocolsChatGPT),
		},
		{
			ProviderID:          ProviderSpecGemini,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Gemini API",
			SetupHint:           "Google AI Studio API key",
			SetupKeywords:       []string{"Gemini", "Google AI Studio", "Google", "Interactions", "Google Search", "MCP"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://generativelanguage.googleapis.com/v1",
			},
			Credential: CredentialSpec{
				Requirement: CredentialOptional, Authoring: CredentialAuthoringAmbientOrReference,
				SuggestedEnvVar: "GEMINI_API_KEY", AmbientLabel: "Google identity (ADC)", ReferenceLabel: "Gemini API key",
			},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsGemini),
		},
		{
			ProviderID:          ProviderSpecAnthropic,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "DeepSeek",
			SetupHint:           "API key",
			SetupKeywords:       []string{"credential", "model", "V4", "Pro", "Flash", "thinking", "web search"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.deepseek.com/anthropic/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "DEEPSEEK_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsDeepSeek),
		},
		{
			ProviderID:          ProviderSpecKimi,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Kimi",
			SetupHint:           "API key",
			SetupKeywords:       []string{"Moonshot", "K3", "K2.7", "thinking", "coding", "credential", "model"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.moonshot.ai/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "MOONSHOT_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsKimi),
		},
		{
			ProviderID:          ProviderSpecMistral,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Mistral AI",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"Mistral", "Vibe", "Devstral", "credential", "model", "EU", "regional", "reasoning"},
			Locator: LocatorSpec{
				Kind:    LocatorBaseURL,
				Label:   "base URL",
				Default: "https://api.mistral.ai/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "MISTRAL_API_KEY"},
			DefaultAuthHeader:   "Authorization",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsMistral),
		},
		{
			ProviderID:          ProviderSpecCerebras,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Cerebras",
			SetupHint:           "API key",
			SetupKeywords:       []string{"Cerebras", "Cerebras Code", "credential", "model", "dedicated endpoint", "reasoning", "coding"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://api.cerebras.ai/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "CEREBRAS_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsCerebras),
		},
		{
			ProviderID:          ProviderSpecWorkersAI,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeNone,
			ProviderDisplayName: "Cloudflare Workers AI",
			SetupHint:           "account endpoint + API token",
			SetupKeywords:       []string{"Cloudflare", "Workers AI", "account ID", "API token", "Chat Completions", "Responses"},
			Locator: LocatorSpec{
				Kind:  LocatorBaseURL,
				Label: "Workers AI base URL",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "CLOUDFLARE_API_TOKEN"},
			DefaultAuthHeader:   "Authorization",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsWorkersAI),
		},
		{
			ProviderID:          ProviderSpecLLM7,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "LLM7",
			SetupHint:           "optional API token",
			SetupKeywords:       []string{"LLM7", "free", "anonymous", "default", "fast", "pro", "token", "model routing"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://api.llm7.io/v1"},
			Credential:          CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "LLM7_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsLLM7),
		},
		{
			ProviderID:          ProviderSpecNVIDIA,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "NVIDIA NIM Hosted",
			SetupHint:           "API key",
			SetupKeywords:       []string{"NVIDIA", "NIM", "API Catalog", "build.nvidia.com", "DGX Cloud"},
			Locator: LocatorSpec{
				Kind:    LocatorFixed,
				Default: "https://integrate.api.nvidia.com/v1",
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "NVIDIA_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsNVIDIA),
		},
		{
			ProviderID:          ProviderSpecOVHCloud,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "OVHcloud AI Endpoints",
			SetupHint:           "optional access token",
			SetupKeywords:       []string{"OVHcloud", "OVH", "AI Endpoints", "free", "anonymous", "OpenAI", "function calling", "Claude Code"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1"},
			Credential:          CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "OVH_AI_ENDPOINTS_ACCESS_TOKEN"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsOVHCloud),
		},
		{
			ProviderID:          ProviderSpecModelScope,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "ModelScope API-Inference",
			SetupHint:           "ModelScope token",
			SetupKeywords:       []string{"ModelScope", "API-Inference", "Alibaba", "free", "coding", "reasoning", "Qwen", "GLM", "DeepSeek", "OpenAI"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://api-inference.modelscope.cn/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "MODELSCOPE_TOKEN"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsModelScope),
		},
		{
			ProviderID:          ProviderSpecRunPod,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Runpod",
			SetupHint:           "endpoint and API key",
			SetupKeywords:       []string{"Runpod", "Serverless", "vLLM", "SGLang", "endpoint", "GPU", "Public Endpoint"},
			Locator:             LocatorSpec{Kind: LocatorBaseURL, Label: "endpoint"},
			Credential:          CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "RUNPOD_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsRunPod),
		},
		{
			ProviderID:          ProviderSpecFriendli,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeNone,
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
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Together AI",
			SetupHint:           "API key",
			SetupKeywords:       []string{"Together", "credential", "model", "dedicated", "reasoning"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://api.together.ai/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "TOGETHER_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsTogether),
		},
		{
			ProviderID:          ProviderSpecDeepInfra,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "DeepInfra",
			SetupHint:           "API token",
			SetupKeywords:       []string{"DeepInfra", "credential", "model", "private deployment", "deploy_id", "Chat Completions"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://api.deepinfra.com/v1/openai"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "DEEPINFRA_TOKEN"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsDeepInfra),
		},
		{
			ProviderID:          ProviderSpecScaleway,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeNone,
			ProviderDisplayName: "Fireworks AI",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"Fireworks", "credential", "serverless", "deployment", "router", "base URL", "Responses", "Messages"},
			Locator:             LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.fireworks.ai/inference/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "FIREWORKS_API_KEY"},
			DefaultAuthHeader:   "Authorization",
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsAllStandard),
		},
		{
			ProviderID:          ProviderSpecSambaNova,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeNone,
			ProviderDisplayName: "Z.AI",
			SetupHint:           "access / API key",
			SetupKeywords:       []string{"access", "General API", "Coding Plan", "credential", "model", "GLM"},
			Locator: LocatorSpec{
				Kind: LocatorFixed,
			},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "ZAI_API_KEY"},
			ConnectionShape:     routing.ConnectionShapeZAI,
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsZAI),
		},
		{
			ProviderID:          ProviderSpecBedrock,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "AWS Bedrock",
			SetupHint:           "region / AWS identity",
			SetupKeywords:       []string{"region", "Bedrock API key", "AWS credentials", "model", "protocol"},
			Locator: LocatorSpec{
				Kind:  LocatorAWSRegion,
				Label: "region",
			},
			Credential: CredentialSpec{
				Requirement: CredentialOptional, Authoring: CredentialAuthoringAmbientOrReference,
				SuggestedEnvVar: "AWS_BEARER_TOKEN_BEDROCK", AmbientLabel: "AWS identity", ReferenceLabel: "Bedrock API key",
			},
			ConnectionShape:     routing.ConnectionShapeBedrock,
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsBedrock),
		},
		{
			ProviderID:          ProviderSpecAzure,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
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
		{
			ProviderID:          ProviderSpecNovita,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Novita AI",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"Novita AI", "serverless", "deployment", "credential", "model", "base URL", "Chat Completions"},
			Locator:             LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.novita.ai/openai/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "NOVITA_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsNovita),
		},
		{
			// Baseten is deliberately a zero-code provider adapter in P0: the
			// standard profile and shared OpenAI-family runtime own its behavior.
			ProviderID:          ProviderSpecBaseten,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "Baseten",
			SetupHint:           "API key / endpoint",
			SetupKeywords:       []string{"Baseten", "Model APIs", "dedicated", "deployment", "credential", "model", "base URL", "Chat Completions", "Messages"},
			Locator:             LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://inference.baseten.co/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "BASETEN_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsBaseten),
		},
		{
			// Hyperbolic identifies managed serverless inference only. A user-run
			// vLLM endpoint on rented Hyperbolic capacity remains provider vLLM.
			ProviderID:          ProviderSpecHyperbolic,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeNone,
			ProviderDisplayName: "Hyperbolic",
			SetupHint:           "API key",
			SetupKeywords:       []string{"Hyperbolic", "serverless", "credential", "model", "Chat Completions"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://api.hyperbolic.xyz/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "HYPERBOLIC_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsHyperbolic),
		},
		{
			ProviderID:          ProviderSpecSiliconFlow,
			ConnectionShape:     routing.ConnectionShapeStandard,
			ModelDiscovery:      ModelDiscoveryModeAdvisory,
			ProviderDisplayName: "SiliconFlow",
			SetupHint:           "API key",
			SetupKeywords:       []string{"SiliconFlow", "SiliconCloud", "credential", "model", "Chat Completions", "Messages"},
			Locator:             LocatorSpec{Kind: LocatorFixed, Default: "https://api.siliconflow.cn/v1"},
			Credential:          CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "SILICONFLOW_API_KEY"},
			VisibleInOperatorUI: true,
			ProviderProtocols:   slices.Clone(providerProtocolsSiliconFlow),
		},
	}
	for _, provider := range profiles {
		if err := ValidateCatalogProfile(provider); err != nil {
			panic(fmt.Sprintf("invalid provider catalog: %v", err))
		}
	}
	return profiles
}
