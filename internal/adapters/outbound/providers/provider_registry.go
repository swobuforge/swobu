package providers

import (
	"context"
	"net/http"

	anthropicprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/anthropic"
	azureprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/azure"
	bedrockprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/bedrock"
	chatgptprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/chatgpt"
	ollamaprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/ollama"
	openaiprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openai"
	openaicompatprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaicompat"
	openrouterprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openrouter"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	providercompat "github.com/swobuforge/swobu/internal/adapters/outbound/providers/providercompat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

// ProviderIngressResolverComposition is the outbound provider registry and dispatch root.
type ProviderIngressResolverComposition struct {
	runtimes map[profile.ProviderID]providersruntime.ProviderRuntimeBundle
}

// NewProviderIngressResolverComposition composes concrete provider adapters once at the composition edge.
func NewProviderIngressResolverComposition(client *http.Client, credentials providersruntime.CredentialProvider, azureProjectEndpoint string) ProviderIngressResolverComposition {
	if client == nil {
		client = http.DefaultClient
	}
	runtimes := map[profile.ProviderID]providersruntime.ProviderRuntimeBundle{
		profile.ProviderSpecOllama:           runtimeForProvider(client, credentials, profile.ProviderSpecOllama, ""),
		profile.ProviderSpecOpenAI:           runtimeForProvider(client, credentials, profile.ProviderSpecOpenAI, ""),
		profile.ProviderSpecOpenRouter:       runtimeForProvider(client, credentials, profile.ProviderSpecOpenRouter, ""),
		profile.ProviderSpecOpenAICompatible: runtimeForProvider(client, credentials, profile.ProviderSpecOpenAICompatible, ""),
		profile.ProviderSpecAnthropic:        runtimeForProvider(client, credentials, profile.ProviderSpecAnthropic, ""),
		profile.ProviderSpecAzure:            runtimeForProvider(client, credentials, profile.ProviderSpecAzure, azureProjectEndpoint),
		profile.ProviderSpecBedrock:          runtimeForProvider(client, credentials, profile.ProviderSpecBedrock, ""),
		profile.ProviderSpecChatGPT:          runtimeForProvider(client, credentials, profile.ProviderSpecChatGPT, ""),
	}
	return ProviderIngressResolverComposition{runtimes: runtimes}
}

func runtimeForProvider(client *http.Client, credentials providersruntime.CredentialProvider, providerID profile.ProviderID, azureProjectEndpoint string) providersruntime.ProviderRuntimeBundle {
	switch providerID {
	case profile.ProviderSpecOllama:
		return ollamaprovider.NewRuntime(client, credentials)
	case profile.ProviderSpecOpenAI:
		return openaiprovider.NewRuntime(client, credentials)
	case profile.ProviderSpecOpenRouter:
		return openrouterprovider.NewRuntime(client, credentials)
	case profile.ProviderSpecOpenAICompatible:
		return openaicompatprovider.NewRuntime(client, credentials)
	case profile.ProviderSpecAnthropic:
		return anthropicprovider.NewRuntime(providerID, client, credentials)
	case profile.ProviderSpecAzure:
		return azureprovider.NewRuntime(client, credentials, azureProjectEndpoint)
	case profile.ProviderSpecBedrock:
		return bedrockprovider.NewRuntime(providerID, client, credentials)
	case profile.ProviderSpecChatGPT:
		return chatgptprovider.NewRuntime(providerID, client, credentials)
	default:
		panic("providers: missing runtime constructor for provider id " + string(providerID))
	}
}

func (r ProviderIngressResolverComposition) ResolveProviderIngress(ctx context.Context, req ports.ProviderRequest) (ports.ProviderIngress, error) {
	runtime, err := r.runtimeForTargetProvider(req.Target.ProviderID())
	if err != nil {
		return nil, err
	}
	if err := providercompat.GateRouteFeatureSupport(ctx, req.EffectSink, req.ExchangeID, string(runtime.ProviderID), string(req.Target.ProtocolKind), req.Request); err != nil {
		return nil, err
	}
	return runtime.IngressResolver.ResolveProviderIngress(ctx, req)
}

func (r ProviderIngressResolverComposition) ListModels(ctx context.Context, target exchange.RoutableTarget) ([]string, error) {
	runtime, err := r.runtimeForTargetProvider(target.ProviderID())
	if err != nil {
		return nil, err
	}
	return runtime.ModelCatalogClient.ListModels(ctx, target)
}

func (r ProviderIngressResolverComposition) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	runtime, err := r.runtimeForTargetProvider(target.ProviderID())
	if err != nil {
		return err
	}
	return runtime.ModelCatalogClient.ValidateCredentials(ctx, target)
}

func (r ProviderIngressResolverComposition) runtimeForTargetProvider(rawProviderID string) (providersruntime.ProviderRuntimeBundle, error) {
	providerID, ok := profile.ParseProviderID(rawProviderID)
	if !ok {
		return providersruntime.ProviderRuntimeBundle{}, canonical.BadEndpoint("provider id is unsupported")
	}
	runtime, ok := r.runtimes[providerID]
	if !ok {
		return providersruntime.ProviderRuntimeBundle{}, canonical.BadEndpoint("provider id is unsupported")
	}
	return runtime, nil
}

var _ ports.ProviderIngressResolver = ProviderIngressResolverComposition{}
var _ ports.ProviderModelCatalog = ProviderIngressResolverComposition{}
