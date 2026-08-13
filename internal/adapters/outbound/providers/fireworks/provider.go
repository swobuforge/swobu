package fireworks

import (
	"context"
	"fmt"
	"net/http"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// NewRuntime composes Fireworks' stored-continuation authority with shared
// protocol codecs and transport. MCP remains Exchange-owned for every target.
// Model identities and configured base URLs remain opaque operator facts.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecFireworks))
	bundle.BackendResolver = responsesBackendResolver{standard: bundle.BackendResolver}
	bundle.Discovery = unsupportedDiscovery{}
	return bundle
}

// unsupportedDiscovery preserves manual Fireworks model authoring until live
// characterization establishes a stable catalog contract for this namespace.
type unsupportedDiscovery struct{}

func (unsupportedDiscovery) ProbeTarget(context.Context, provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	return provider.TargetProbeResult{}, canonical.NotImplemented("Swobu does not implement Fireworks model discovery")
}

type responsesBackendResolver struct{ standard provider.BackendResolver }

func (r responsesBackendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil || target.ProtocolKind != protocolkind.Responses {
		return backend, err
	}
	codec, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, fmt.Errorf("Fireworks responses backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	// Fireworks documents reusable Responses IDs only when response storage is
	// permitted. Codec decoding applies that request-local eligibility rule.
	codec.CaptureResponsesContinuation = true
	backend.Codec = codec
	return backend, backend.Validate()
}

var _ provider.Discovery = unsupportedDiscovery{}
