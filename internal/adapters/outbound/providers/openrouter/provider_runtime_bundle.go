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
	if target.ProtocolKind == protocolkind.ChatCompletions {
		standard, ok := backend.Codec.(protocolcodec.Codec)
		if !ok {
			return provider.Backend{}, fmt.Errorf("OpenRouter chat completions backend has codec %T, want protocolcodec.Codec", backend.Codec)
		}
		backend.Codec = reasoningCodec{standard: standard}
	} else if target.ProtocolKind == protocolkind.Responses {
		standard, ok := backend.Codec.(protocolcodec.Codec)
		if !ok {
			return provider.Backend{}, fmt.Errorf("OpenRouter responses backend has codec %T, want protocolcodec.Codec", backend.Codec)
		}
		backend.Codec = responsesCodec{standard: standard}
	}
	return backend, backend.Validate()
}
