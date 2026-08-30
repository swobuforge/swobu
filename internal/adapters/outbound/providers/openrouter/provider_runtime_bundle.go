package openrouter

import (
	"fmt"
	"net/http"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecOpenRouter))
	bundle.BackendResolver = reasoningBackendResolver{standard: bundle.BackendResolver}
	return bundle
}

type reasoningBackendResolver struct{ standard provider.BackendResolver }

func (r reasoningBackendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	switch target.ProtocolKind {
	case protocolkind.ChatCompletions:
		backend.Codec = protocolcodec.Codec{
			Protocol: protocolkind.ChatCompletions,
			ChatDialect: protocolcodec.ChatDialect{
				Lowering: protocolcodec.ChatLowering{
					Tools:     protocolcodec.ChatToolLowering{WebSearch: protocolcodec.ChatHostedSearchTool(nil, "openrouter:web_search")},
					Reasoning: applyOpenRouterReasoning,
					Message:   protocolcodec.ChatOpaqueReplayJSONMessageRule(ChatReplayScope, "reasoning_details"),
				},
				DecorateAttempt:   decorateOpenRouterAttempt,
				ResponseReasoning: func() protocolcodec.ChatReasoningExtractor { return &openRouterReasoningExtractor{} },
			},
		}
	case protocolkind.Responses:
		backend.Codec = protocolcodec.Codec{
			Protocol: protocolkind.Responses,
			ResponsesDialect: protocolcodec.ResponsesDialect{
				Tools:           protocolcodec.ResponsesToolLowering{WebSearch: protocolcodec.ResponsesHostedSearchTool("openrouter:web_search", false)},
				DecorateAttempt: decorateOpenRouterAttempt,
			},
		}
	default:
		return provider.Backend{}, fmt.Errorf("OpenRouter backend protocol %q is unsupported", target.ProtocolKind)
	}
	return backend, backend.Validate()
}
