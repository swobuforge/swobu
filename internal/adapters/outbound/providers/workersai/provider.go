package workersai

import (
	"context"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// NewRuntime composes shared OpenAI-family codecs and transport with only the
// Workers AI provider-product boundary. Model authoring remains manual until a
// live stable model-search schema earns a provider-local decoder.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.WorkersAIPolicy())
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	bundle.Discovery = unsupportedDiscovery{}
	return bundle
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	// @cf/ establishes Workers AI product and billing provenance only. It says
	// nothing about this model's protocol, tool, reasoning, or format support.
	if !strings.HasPrefix(target.Model, "@cf/") {
		return provider.Backend{}, canonical.BadEndpoint("workersai provider requires a Workers AI @cf/ model identity")
	}
	return r.standard.ResolveBackend(target)
}

type unsupportedDiscovery struct{}

func (unsupportedDiscovery) ProbeTarget(context.Context, provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	return provider.TargetProbeResult{}, canonical.NotImplemented("Swobu does not implement Workers AI model discovery")
}

var _ provider.Discovery = unsupportedDiscovery{}
