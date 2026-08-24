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
				LowerTool:         protocolcodec.ChatHostedSearchTool(nil, "openrouter:web_search"),
				LowerToolPolicy:   protocolcodec.ChatHostedSearchToolPolicy("openrouter:web_search"),
				LowerReasoning:    applyOpenRouterReasoning,
				LowerMessage:      protocolcodec.ChatOpaqueReplayJSONMessageRule(ChatReplayScope, "reasoning_details"),
				DecorateAttempt:   decorateOpenRouterAttempt,
				ResponseReasoning: func() protocolcodec.ChatReasoningExtractor { return &openRouterReasoningExtractor{} },
			},
		}
	case protocolkind.Responses:
		backend.Codec = protocolcodec.Codec{
			Protocol: protocolkind.Responses,
			ResponsesDialect: protocolcodec.ResponsesDialect{
				LowerTool:       protocolcodec.ResponsesHostedSearchTool("openrouter:web_search"),
				LowerToolPolicy: protocolcodec.ResponsesHostedSearchToolPolicy("openrouter:web_search"),
				DecorateAttempt: decorateOpenRouterAttempt,
			},
		}
	default:
		return provider.Backend{}, fmt.Errorf("OpenRouter backend protocol %q is unsupported", target.ProtocolKind)
	}
	return backend, backend.Validate()
}
