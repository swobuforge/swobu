package friendli

import (
	"context"
	"fmt"
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// NewRuntime composes Friendli's endpoint-agnostic Bearer transport with the
// shared Chat Completions, Responses, and Messages codecs. Friendli model
// tokens name materially different deployment resources, so model discovery is
// intentionally unavailable and operators retain exact manual entry.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecFriendli))
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	bundle.Discovery = unsupportedDiscovery{}
	return bundle
}

type unsupportedDiscovery struct{}

func (unsupportedDiscovery) ProbeTarget(context.Context, provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	return provider.TargetProbeResult{}, canonical.NotImplemented("Swobu does not implement Friendli model discovery")
}

// backendResolver decorates only the shared Chat grammar. Responses and
// Messages stay exact standard-codec composition because Friendli has no
// demonstrated differing wire semantics for those protocol families.
type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil || target.ProtocolKind != protocolkind.ChatCompletions {
		return backend, err
	}
	standard, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, fmt.Errorf("Friendli Chat backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	backend.Codec = reasoningCodec{standard: standard}
	return backend, backend.Validate()
}

var _ provider.Discovery = unsupportedDiscovery{}
