package providers

import (
	"context"
	"fmt"
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
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

// ProviderIngressResolverComposition is the outbound provider composition and dispatch owner.
type ProviderIngressResolverComposition struct {
	ollama           providersruntime.ProviderRuntimeBundle
	openai           providersruntime.ProviderRuntimeBundle
	openrouter       providersruntime.ProviderRuntimeBundle
	openaiCompatible providersruntime.ProviderRuntimeBundle
	anthropic        providersruntime.ProviderRuntimeBundle
	azure            providersruntime.ProviderRuntimeBundle
	bedrock          providersruntime.ProviderRuntimeBundle
	chatgpt          providersruntime.ProviderRuntimeBundle
}

// NewProviderIngressResolverComposition composes concrete provider adapters once at the composition edge.
func NewProviderIngressResolverComposition(client *http.Client, credentials providersruntime.CredentialProvider, azureProjectEndpoint string) ProviderIngressResolverComposition {
	if client == nil {
		client = http.DefaultClient
	}
	build := func(providerID profile.ProviderID) providersruntime.ProviderRuntimeBundle {
		runtime := runtimeForProvider(client, credentials, providerID, "")
		validateRuntimeAgainstProfile(profile.Profile{ProviderID: providerID}, runtime)
		return runtime
	}
	return ProviderIngressResolverComposition{
		ollama:           build(profile.ProviderSpecOllama),
		openai:           build(profile.ProviderSpecOpenAI),
		openrouter:       build(profile.ProviderSpecOpenRouter),
		openaiCompatible: build(profile.ProviderSpecOpenAICompatible),
		anthropic:        build(profile.ProviderSpecAnthropic),
		azure:            runtimeForProvider(client, credentials, profile.ProviderSpecAzure, azureProjectEndpoint),
		bedrock:          build(profile.ProviderSpecBedrock),
		chatgpt:          build(profile.ProviderSpecChatGPT),
	}
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

func (r ProviderIngressResolverComposition) ResolveProviderIngress(ctx context.Context, req ports.ProviderRequest) (ports.ProviderIngress, error) {
	runtime, err := r.runtimeForTargetProvider(req.Target.ProviderID())
	if err != nil {
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
	switch providerID {
	case profile.ProviderSpecOllama:
		return r.ollama, nil
	case profile.ProviderSpecOpenAI:
		return r.openai, nil
	case profile.ProviderSpecOpenRouter:
		return r.openrouter, nil
	case profile.ProviderSpecOpenAICompatible:
		return r.openaiCompatible, nil
	case profile.ProviderSpecAnthropic:
		return r.anthropic, nil
	case profile.ProviderSpecAzure:
		return r.azure, nil
	case profile.ProviderSpecBedrock:
		return r.bedrock, nil
	case profile.ProviderSpecChatGPT:
		return r.chatgpt, nil
	default:
		return providersruntime.ProviderRuntimeBundle{}, canonical.BadEndpoint("provider id is unsupported")
	}
}
