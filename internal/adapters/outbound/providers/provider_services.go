package providers

import (
	"context"
	"net/http"

	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

// ProviderExecutorService dispatches canonical execution by configured provider id.
type ProviderExecutorService struct {
	byProviderID map[profile.ProviderID]providersruntime.Executor
}

// ProviderModelCatalogService dispatches model-catalog loading by configured provider id.
type ProviderModelCatalogService struct {
	byProviderID map[profile.ProviderID]providersruntime.ModelCatalogClient
}

// ProviderServicesBundle groups provider lifecycle services built from one provider-definition registry.
type ProviderServicesBundle struct {
	Execution    ProviderExecutorService
	ModelCatalog ProviderModelCatalogService
}

// NewProviderServicesBundle is the single composition entrypoint for outbound provider lifecycle services.
func NewProviderServicesBundle(client *http.Client, credentials providersruntime.CredentialProvider) ProviderServicesBundle {
	runtimes := NewRuntimeFactory(client, credentials).Build(profile.All())
	execution := make(map[profile.ProviderID]providersruntime.Executor, len(runtimes))
	modelCatalog := make(map[profile.ProviderID]providersruntime.ModelCatalogClient, len(runtimes))
	for providerID, runtime := range runtimes {
		execution[providerID] = runtime.Executor
		modelCatalog[providerID] = runtime.ModelCatalogClient
	}
	return ProviderServicesBundle{
		Execution: ProviderExecutorService{
			byProviderID: execution,
		},
		ModelCatalog: ProviderModelCatalogService{
			byProviderID: modelCatalog,
		},
	}
}

func (s ProviderExecutorService) Execute(ctx context.Context, req ports.ProviderRequest) (ports.ProviderTransportResponse, error) {
	providerID, ok := profile.ParseProviderID(req.Target.ProviderID())
	if !ok {
		return ports.ProviderTransportResponse{}, canonical.BadEndpoint("provider id is unsupported")
	}
	adapter, ok := s.byProviderID[providerID]
	if !ok {
		return ports.ProviderTransportResponse{}, canonical.BadEndpoint("provider id is unsupported")
	}
	return adapter.Execute(ctx, req)
}

func (s ProviderModelCatalogService) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	providerID, ok := profile.ParseProviderID(target.ProviderID())
	if !ok {
		return nil, canonical.BadEndpoint("provider id is unsupported")
	}
	adapter, ok := s.byProviderID[providerID]
	if !ok {
		return nil, canonical.BadEndpoint("provider id is unsupported")
	}
	return adapter.ListModels(ctx, target)
}

func (s ProviderModelCatalogService) ValidateCredentials(ctx context.Context, target ports.RoutableTarget) error {
	providerID, ok := profile.ParseProviderID(target.ProviderID())
	if !ok {
		return canonical.BadEndpoint("provider id is unsupported")
	}
	adapter, ok := s.byProviderID[providerID]
	if !ok {
		return canonical.BadEndpoint("provider id is unsupported")
	}
	return adapter.ValidateCredentials(ctx, target)
}
