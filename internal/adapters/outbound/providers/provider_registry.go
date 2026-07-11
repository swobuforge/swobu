package providers

import (
	"context"
	"fmt"
	"net/http"

	anthropicprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/anthropic"
	bedrockprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/bedrock"
	chatgptprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/chatgpt"
	ollamaprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/ollama"
	openaiprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openai"
	openaicompatprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaicompat"
	openrouterprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openrouter"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

// ProviderIngressResolverRegistry is the single outbound provider composition and dispatch owner.
type ProviderIngressResolverRegistry struct {
	byProviderID map[profile.ProviderID]providersruntime.ProviderRuntimeBundle
}

// NewProviderRegistry composes concrete provider adapters once at the composition root.
func NewProviderRegistry(client *http.Client, credentials providersruntime.CredentialProvider) ProviderIngressResolverRegistry {
	if client == nil {
		client = http.DefaultClient
	}
	byProviderID := make(map[profile.ProviderID]providersruntime.ProviderRuntimeBundle, len(profile.All()))
	for _, providerProfile := range profile.All() {
		providerID := providerProfile.ProviderID
		if providerID == "" {
			panic("providers: empty provider id in registry entry")
		}
		if _, exists := byProviderID[providerID]; exists {
			panic("providers: duplicate provider runtime for provider id " + string(providerID))
		}
		runtime := runtimeForProvider(client, credentials, providerID)
		validateRuntimeAgainstProfile(providerProfile, runtime)
		byProviderID[providerID] = runtime
	}
	return ProviderIngressResolverRegistry{byProviderID: byProviderID}
}

func runtimeForProvider(client *http.Client, credentials providersruntime.CredentialProvider, providerID profile.ProviderID) providersruntime.ProviderRuntimeBundle {
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
	case profile.ProviderSpecBedrock:
		return bedrockprovider.NewRuntime(providerID, client, credentials)
	case profile.ProviderSpecChatGPT:
		return chatgptprovider.NewRuntime(providerID, client, credentials)
	default:
		panic("providers: missing runtime constructor for provider id " + string(providerID))
	}
}

func validateRuntimeAgainstProfile(providerProfile profile.Profile, runtime providersruntime.ProviderRuntimeBundle) {
	providerID := providerProfile.ProviderID
	if runtime.ProviderID != providerID {
		panic(fmt.Sprintf("providers: runtime id mismatch for %s", providerID))
	}
	if runtime.IngressResolver == nil {
		panic(fmt.Sprintf("providers: missing ingress resolver for provider id %s", providerID))
	}
	if runtime.CredentialProvider == nil {
		panic(fmt.Sprintf("providers: missing credential provider for provider id %s", providerID))
	}
	if profile.SupportsCapability(string(providerID), profile.CapabilityModelCatalog) && runtime.ModelCatalogClient == nil {
		panic(fmt.Sprintf("providers: model catalog capability declared without ModelCatalogClient for provider id %s", providerID))
	}
}

func (r ProviderIngressResolverRegistry) ResolveProviderIngress(ctx context.Context, req ports.ProviderRequest) (ports.ProviderIngress, error) {
	runtime, err := r.runtimeForTargetProvider(req.Target.ProviderID())
	if err != nil {
		return nil, err
	}
	return runtime.IngressResolver.ResolveProviderIngress(ctx, req)
}

func (r ProviderIngressResolverRegistry) ListModels(ctx context.Context, target exchange.RoutableTarget) ([]string, error) {
	runtime, err := r.runtimeForTargetProvider(target.ProviderID())
	if err != nil {
		return nil, err
	}
	return runtime.ModelCatalogClient.ListModels(ctx, target)
}

func (r ProviderIngressResolverRegistry) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	runtime, err := r.runtimeForTargetProvider(target.ProviderID())
	if err != nil {
		return err
	}
	return runtime.ModelCatalogClient.ValidateCredentials(ctx, target)
}

func (r ProviderIngressResolverRegistry) runtimeForTargetProvider(rawProviderID string) (providersruntime.ProviderRuntimeBundle, error) {
	providerID, ok := profile.ParseProviderID(rawProviderID)
	if !ok {
		return providersruntime.ProviderRuntimeBundle{}, canonical.BadEndpoint("provider id is unsupported")
	}
	runtime, ok := r.byProviderID[providerID]
	if !ok {
		return providersruntime.ProviderRuntimeBundle{}, canonical.BadEndpoint("provider id is unsupported")
	}
	return runtime, nil
}
