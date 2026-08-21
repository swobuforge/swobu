// Package zai owns the two-access Z.AI provider and its demonstrated
// divergences from standard Chat Completions request grammar.
package zai

import (
	"context"
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// NewRuntime composes Z.AI Bearer transport, manual model entry, and the exact
// hosted-search request rewrite for both access products.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecZAI))
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	bundle.Discovery = unsupportedDiscovery{}
	return bundle
}

type unsupportedDiscovery struct{}

func (unsupportedDiscovery) ProbeTarget(context.Context, provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	return provider.TargetProbeResult{}, canonical.NotImplemented("Swobu does not implement Z.AI model discovery")
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	backend.Codec = protocolcodec.Codec{
		Protocol: protocolkind.ChatCompletions,
		ChatDialect: protocolcodec.ChatDialect{
			LowerTool: protocolcodec.ChatHostedSearchTool(func() any {
				return requestTool{Type: canonical.ToolTypeWebSearch, WebSearch: &webSearchOptions{Enable: true}}
			}, canonical.ToolTypeWebSearch),
			LowerToolPolicy: protocolcodec.ChatHostedSearchToolPolicy(canonical.ToolTypeWebSearch),
			LowerReasoning:  applyReasoning,
		},
	}
	return backend, backend.Validate()
}

type requestTool struct {
	Type      string            `json:"type"`
	WebSearch *webSearchOptions `json:"web_search,omitempty"`
}

type webSearchOptions struct {
	Enable bool `json:"enable"`
}

var _ provider.Discovery = unsupportedDiscovery{}
