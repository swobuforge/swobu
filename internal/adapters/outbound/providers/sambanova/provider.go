package sambanova

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

// NewRuntime keeps SambaCloud and SambaStack on the shared protocol and
// transport paths, decorating only the documented Messages thinking carrier.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecSambaNova))
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
	case protocolkind.ChatCompletions, protocolkind.Responses:
		return backend, backend.Validate()
	case protocolkind.Messages:
		backend.Codec = protocolcodec.Codec{
			Protocol:        protocolkind.Messages,
			MessagesDialect: protocolcodec.MessagesDialect{Lowering: protocolcodec.MessagesLowering{Reasoning: protocolcodec.MessagesOmitAdaptiveReasoning}},
		}
		return backend, backend.Validate()
	default:
		return provider.Backend{}, fmt.Errorf("SambaNova backend protocol %q is unsupported", target.ProtocolKind)
	}
}
