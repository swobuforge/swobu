package novita

import (
	"fmt"
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// NewRuntime composes Novita's shared Bearer transport and model catalog with
// the exact Chat reasoning_details replay refinement.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecNovita))
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	return bundle
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	if target.ProtocolKind != protocolkind.ChatCompletions {
		return provider.Backend{}, fmt.Errorf("Novita backend protocol %q is not Chat Completions", target.ProtocolKind)
	}
	backend.Codec = protocolcodec.Codec{
		Protocol: protocolkind.ChatCompletions,
		ChatDialect: protocolcodec.ChatDialect{
			Lowering: protocolcodec.ChatLowering{Message: protocolcodec.ChatOpaqueReplayJSONMessageRule(ChatReplayScope, "reasoning_details")},
			ResponseReasoning: func() protocolcodec.ChatReasoningExtractor {
				return &reasoningDetailsExtractor{}
			},
		},
	}
	return backend, backend.Validate()
}
