// Package ollama composes Ollama's OpenAI-compatible runtime with its narrow
// Responses request-grammar normalization.
package ollama

import (
	"net/http"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// NewRuntime conservatively places current Responses instructions before full
// replay history for every Ollama target. This provider-wide normalization
// avoids model-name inference while preserving ordinary OpenAI-family behavior
// for Chat Completions and other providers.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardNoAuthPolicy(profile.ProviderSpecOllama))
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	return bundle
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	if target.ProtocolKind == protocolkind.Responses {
		backend.Codec = protocolcodec.Codec{
			Protocol: protocolkind.Responses,
			ResponsesDialect: protocolcodec.ResponsesDialect{
				PrependInstructionsToInput: true,
			},
		}
	}
	return backend, backend.Validate()
}
