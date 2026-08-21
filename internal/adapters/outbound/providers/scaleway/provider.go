package scaleway

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

// NewRuntime composes Scaleway's OpenAI-compatible transport and catalog with
// the two small documented differences in its stateless Responses and Chat
// reasoning carriers. A base URL and credential are target facts, not modes.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecScaleway))
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	return bundle
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	switch target.ProtocolKind {
	case protocolkind.ChatCompletions:
		backend.Codec = protocolcodec.Codec{
			Protocol: protocolkind.ChatCompletions,
			ChatDialect: protocolcodec.ChatDialect{
				ResponseReasoning: func() protocolcodec.ChatReasoningExtractor { return scalewayChatReasoningExtractor{} },
			},
		}
	case protocolkind.Responses:
		backend.Codec = protocolcodec.Codec{
			Protocol: protocolkind.Responses,
			ResponsesDialect: protocolcodec.ResponsesDialect{
				PrependInstructionsToInput: true,
				OmitInclude:                true,
				OmitStoreFalse:             true,
			},
		}
	default:
		return provider.Backend{}, fmt.Errorf("Scaleway backend protocol %q is unsupported", target.ProtocolKind)
	}
	return backend, backend.Validate()
}
