package providers

import (
	"context"
	"net/http"

	anthropicprovider "github.com/metrofun/swobu/internal/adapters/outbound/providers/anthropic"
	customprovider "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/providercatalog"
	"github.com/metrofun/swobu/internal/ports"
)

type CredentialResolver interface {
	ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error)
}

// ProviderExecutorMux dispatches provider execution and model-catalog calls
// by selected provider spec.
type ProviderExecutorMux struct {
	custom    customprovider.ProviderExecutorAdapter
	anthropic anthropicprovider.ProviderExecutorAdapter
}

func NewExecutor(client *http.Client, credentials CredentialResolver) ProviderExecutorMux {
	return ProviderExecutorMux{
		custom:    customprovider.NewExecutor(client, credentials),
		anthropic: anthropicprovider.NewExecutor(client, credentials),
	}
}

func (r ProviderExecutorMux) Execute(ctx context.Context, req ports.ExecuteRequest) (ports.ExecuteResponse, error) {
	switch providerAdapterGroup(req.Target.ProviderSpecName()) {
	case providercatalog.AdapterAnthropicMessages:
		return r.anthropic.Execute(ctx, req)
	case providercatalog.AdapterCustomOpenAICompatible:
		return r.custom.Execute(ctx, req)
	default:
		return ports.ExecuteResponse{}, compatibility.BadEndpoint("provider spec is unsupported")
	}
}

func (r ProviderExecutorMux) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	switch providerAdapterGroup(target.ProviderSpecName()) {
	case providercatalog.AdapterAnthropicMessages:
		return r.anthropic.ListModels(ctx, target)
	case providercatalog.AdapterCustomOpenAICompatible:
		return r.custom.ListModels(ctx, target)
	default:
		return nil, compatibility.BadEndpoint("provider spec is unsupported")
	}
}

func providerAdapterGroup(providerSpec string) string {
	group, _ := providercatalog.AdapterForSpec(providerSpec)
	return group
}
