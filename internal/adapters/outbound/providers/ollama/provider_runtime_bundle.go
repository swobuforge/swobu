package ollama

import (
	"fmt"
	"net/http"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/provider"
)

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.NewOllamaPolicy())
	bundle.BackendResolver = ollamaBackendResolver{standard: bundle.BackendResolver}
	return bundle
}

type ollamaBackendResolver struct{ standard provider.BackendResolver }

func (r ollamaBackendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	standard, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, fmt.Errorf("ollama backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	backend.Codec = webSearchBackendCodec{standard: standard}
	return backend, backend.Validate()
}
