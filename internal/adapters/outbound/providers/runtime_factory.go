package providers

import (
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
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

// RuntimeFactory composes concrete per-provider runtime strategies from domain registry entries.
type RuntimeFactory struct {
	client             *http.Client
	credentialProvider providersruntime.CredentialProvider
}

func NewRuntimeFactory(client *http.Client, credentialProvider providersruntime.CredentialProvider) RuntimeFactory {
	if client == nil {
		client = http.DefaultClient
	}
	return RuntimeFactory{client: client, credentialProvider: credentialProvider}
}

func (f RuntimeFactory) Build(registry []providercatalog.Profile) map[providercatalog.ProviderID]providersruntime.ProviderRuntimeBundle {
	byProviderID := make(map[providercatalog.ProviderID]providersruntime.ProviderRuntimeBundle, len(registry))
	for _, profile := range registry {
		providerID := profile.ProviderID
		if providerID == "" {
			panic("providers: empty provider id in registry entry")
		}
		if _, exists := byProviderID[providerID]; exists {
			panic("providers: duplicate provider runtime for provider id " + string(providerID))
		}
		runtime := f.runtimeFor(providerID)
		validateRuntimeAgainstProfile(profile, runtime)
		byProviderID[providerID] = runtime
	}
	return byProviderID
}

func (f RuntimeFactory) runtimeFor(providerID providercatalog.ProviderID) providersruntime.ProviderRuntimeBundle {
	switch providerID {
	case providercatalog.ProviderSpecOllama:
		return ollamaprovider.NewRuntime(f.client, f.credentialProvider)
	case providercatalog.ProviderSpecOpenAI:
		return openaiprovider.NewRuntime(f.client, f.credentialProvider)
	case providercatalog.ProviderSpecOpenRouter:
		return openrouterprovider.NewRuntime(f.client, f.credentialProvider)
	case providercatalog.ProviderSpecOpenAICompatible:
		return openaicompatprovider.NewRuntime(f.client, f.credentialProvider)
	case providercatalog.ProviderSpecAnthropic:
		return anthropicprovider.NewRuntime(providerID, f.client, f.credentialProvider)
	case providercatalog.ProviderSpecBedrock:
		return bedrockprovider.NewRuntime(providerID, f.client, f.credentialProvider)
	case providercatalog.ProviderSpecChatGPT:
		return chatgptprovider.NewRuntime(providerID, f.client, f.credentialProvider)
	default:
		panic("providers: missing runtime constructor for provider id " + string(providerID))
	}
}

func validateRuntimeAgainstProfile(profile providercatalog.Profile, runtime providersruntime.ProviderRuntimeBundle) {
	providerID := profile.ProviderID
	if runtime.ProviderID != providerID {
		panic(fmt.Sprintf("providers: runtime id mismatch for %s", providerID))
	}
	if runtime.Executor == nil {
		panic(fmt.Sprintf("providers: missing executor for provider id %s", providerID))
	}
	if runtime.CredentialProvider == nil {
		panic(fmt.Sprintf("providers: missing credential provider for provider id %s", providerID))
	}
	if providercatalog.SupportsCapability(string(providerID), providercatalog.CapabilityModelCatalog) && runtime.ModelCatalogClient == nil {
		panic(fmt.Sprintf("providers: model catalog capability declared without ModelCatalogClient for provider id %s", providerID))
	}
}
