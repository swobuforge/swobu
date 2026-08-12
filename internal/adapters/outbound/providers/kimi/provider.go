package kimi

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

// NewRuntime composes Kimi's replay-required reasoning dialect around shared
// OpenAI-family transport, discovery, and Chat Completions grammar.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecKimi))
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
		return provider.Backend{}, fmt.Errorf("Kimi backend protocol %q is not Chat Completions", target.ProtocolKind)
	}
	standard, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, fmt.Errorf("Kimi Chat backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	backend.Codec = reasoningCodec{standard: standard}
	return backend, backend.Validate()
}
