package providers

import (
	"context"
	"net/http"

	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

// ProviderExecutionService dispatches canonical execution by configured provider id.
type ProviderExecutionService struct {
	byProviderID map[providercatalog.ProviderID]providersruntime.Executor
}

// ProviderModelCatalogService dispatches model-catalog loading by configured provider id.
type ProviderModelCatalogService struct {
	byProviderID map[providercatalog.ProviderID]providersruntime.ModelCatalogClient
}

// Services groups provider lifecycle services built from one provider-definition registry.
type Services struct {
	Execution    ProviderExecutionService
	ModelCatalog ProviderModelCatalogService
}

// NewServices is the single composition entrypoint for outbound provider lifecycle services.
func NewServices(client *http.Client, credentials providersruntime.CredentialProvider) Services {
	runtimes := NewRuntimeFactory(client, credentials).Build(providercatalog.All())
	execution := make(map[providercatalog.ProviderID]providersruntime.Executor, len(runtimes))
	modelCatalog := make(map[providercatalog.ProviderID]providersruntime.ModelCatalogClient, len(runtimes))
	for providerID, runtime := range runtimes {
		execution[providerID] = runtime.Executor
		modelCatalog[providerID] = runtime.ModelCatalogClient
	}
	return Services{
		Execution: ProviderExecutionService{
			byProviderID: execution,
		},
		ModelCatalog: ProviderModelCatalogService{
			byProviderID: modelCatalog,
		},
	}
}

func (s ProviderExecutionService) Execute(ctx context.Context, req ports.ProviderRequest) (ports.ProviderResponse, error) {
	providerID, ok := providercatalog.ParseProviderID(req.Target.ProviderID())
	if !ok {
		return ports.ProviderResponse{}, canonical.BadEndpoint("provider id is unsupported")
	}
	adapter, ok := s.byProviderID[providerID]
	if !ok {
		return ports.ProviderResponse{}, canonical.BadEndpoint("provider id is unsupported")
	}
	return adapter.Execute(ctx, req)
}

func (s ProviderModelCatalogService) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	providerID, ok := providercatalog.ParseProviderID(target.ProviderID())
	if !ok {
		return nil, canonical.BadEndpoint("provider id is unsupported")
	}
	adapter, ok := s.byProviderID[providerID]
	if !ok {
		return nil, canonical.BadEndpoint("provider id is unsupported")
	}
	return adapter.ListModels(ctx, target)
}
